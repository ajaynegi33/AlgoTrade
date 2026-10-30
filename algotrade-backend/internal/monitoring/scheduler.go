package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// EODScheduler handles automated scheduling at 15:45 IST and manual triggers
type EODScheduler struct {
	db          *sql.DB
	reportsDir  string
	lastRanDate string
	mu          sync.Mutex
	isStarted   bool
	cancel      context.CancelFunc
	waitGroup   sync.WaitGroup
}

// NewEODScheduler creates a new scheduler for daily EOD reports
func NewEODScheduler(db *sql.DB, reportsDir string) *EODScheduler {
	if reportsDir == "" {
		reportsDir = "reports"
	}
	return &EODScheduler{
		db:         db,
		reportsDir: reportsDir,
	}
}

// Start spawns the background clock watcher checking for 15:45 IST on weekdays
func (scheduler *EODScheduler) Start(parentCtx context.Context) {
	scheduler.mu.Lock()
	if scheduler.isStarted {
		scheduler.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	scheduler.cancel = cancel
	scheduler.isStarted = true
	scheduler.mu.Unlock()

	scheduler.waitGroup.Add(1)
	go scheduler.watchLoop(ctx)
	slog.Info("EODScheduler started (monitoring for 15:45 IST triggers)")
}

// Stop shuts down the background watcher gracefully
func (scheduler *EODScheduler) Stop() {
	scheduler.mu.Lock()
	if !scheduler.isStarted {
		scheduler.mu.Unlock()
		return
	}
	scheduler.cancel()
	scheduler.isStarted = false
	scheduler.mu.Unlock()

	scheduler.waitGroup.Wait()
	slog.Info("EODScheduler stopped")
}

func (scheduler *EODScheduler) watchLoop(ctx context.Context) {
	defer scheduler.waitGroup.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(loc)
			weekday := now.Weekday()

			// Run on weekdays (Monday through Friday)
			if weekday >= time.Monday && weekday <= time.Friday {
				// 15:45 IST (3:45 PM) or later, but before market opens next day
				if (now.Hour() == 15 && now.Minute() >= 45) || (now.Hour() > 15 && now.Hour() <= 23) {
					todayStr := now.Format("2006-01-02")

					scheduler.mu.Lock()
					alreadyRan := scheduler.lastRanDate == todayStr
					if !alreadyRan {
						scheduler.lastRanDate = todayStr
					}
					scheduler.mu.Unlock()

					if !alreadyRan {
						slog.Info("15:45 IST reached; triggering automated EOD diagnostic pipeline", "date", todayStr)
						go func(date string) {
							subCtx, subCancel := context.WithTimeout(context.Background(), 2*time.Minute)
							defer subCancel()
							if _, runErr := scheduler.RunPipeline(subCtx, date); runErr != nil {
								slog.Error("Automated EOD pipeline failed", "date", date, "err", runErr)
							}
						}(todayStr)
					}
				}
			}
		}
	}
}

// TriggerManualReport runs the full diagnostic pipeline on demand for any date
func (scheduler *EODScheduler) TriggerManualReport(ctx context.Context, tradingDate string) (*EODReportSummary, error) {
	if tradingDate == "" {
		loc, err := time.LoadLocation("Asia/Kolkata")
		if err != nil {
			loc = time.FixedZone("IST", 5*3600+1800)
		}
		tradingDate = time.Now().In(loc).Format("2006-01-02")
	}
	return scheduler.RunPipeline(ctx, tradingDate)
}

// RunPipeline executes the complete 6-stage EOD diagnostic and reporting pipeline
func (scheduler *EODScheduler) RunPipeline(ctx context.Context, tradingDate string) (*EODReportSummary, error) {
	slog.Info("Starting EOD Diagnostic Pipeline", "date", tradingDate)

	// Stage 1: Resolve pending shadow signals
	resolvedCount, err := ResolveShadowSignals(scheduler.db, tradingDate)
	if err != nil {
		slog.Warn("Shadow signal resolution encountered warning", "err", err)
	} else {
		slog.Info("Shadow signals evaluated", "resolved_count", resolvedCount)
	}

	// Stage 2: Compute comprehensive EOD metrics
	summary, err := ComputeEODMetrics(scheduler.db, tradingDate)
	if err != nil {
		return nil, fmt.Errorf("failed to compute EOD metrics: %w", err)
	}

	// Stage 3 & 4: Save HTML and Markdown report files
	htmlPath, mdPath, err := SaveReportFiles(scheduler.db, summary, scheduler.reportsDir)
	if err != nil {
		slog.Error("Failed to save EOD report files", "err", err)
	} else {
		slog.Info("EOD report artifacts generated successfully", "html", htmlPath, "markdown", mdPath)
	}

	// Stage 5: Dispatch HTML report via Resend Email API
	htmlContent := GenerateHTMLReport(summary)
	if emailErr := SendEODReportEmail(ctx, scheduler.db, "", "", tradingDate, htmlContent); emailErr != nil {
		slog.Error("Failed to send EOD report email via Resend", "err", emailErr)
	}

	return summary, nil
}

