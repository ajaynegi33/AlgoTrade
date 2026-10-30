package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/trace"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	globalFlightRecorderMutex   sync.RWMutex
	globalFlightRecorderManager *FlightRecorderManager
	reasonSanitizationRegex     = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)
)

// SetGlobalRecorder sets the process-wide FlightRecorderManager singleton
func SetGlobalRecorder(manager *FlightRecorderManager) {
	globalFlightRecorderMutex.Lock()
	defer globalFlightRecorderMutex.Unlock()
	globalFlightRecorderManager = manager
}

// GetGlobalRecorder returns the process-wide FlightRecorderManager singleton
func GetGlobalRecorder() *FlightRecorderManager {
	globalFlightRecorderMutex.RLock()
	defer globalFlightRecorderMutex.RUnlock()
	return globalFlightRecorderManager
}

// RecordLatency is the zero-overhead, completely non-blocking entry point called by trading hot paths.
// It discards calls if uninitialized, disabled, within startup grace period, within cooldown, or below threshold.
func RecordLatency(triggerReason string, measuredDuration, thresholdDuration time.Duration) {
	activeManager := GetGlobalRecorder()
	if activeManager == nil {
		return
	}
	activeManager.RecordLatency(triggerReason, measuredDuration, thresholdDuration)
}

// FlightRecorderManager manages continuous Go runtime trace circular recording and watchdog snapshots
type FlightRecorderManager struct {
	configuration        FlightRecorderConfig
	runtimeRecorder      *trace.FlightRecorder
	dumpRequestChannel   chan DumpRequest
	lifecycleContext     context.Context
	lifecycleCancel      context.CancelFunc
	backgroundWaitGroup  sync.WaitGroup
	serviceStartedAt     time.Time
	lastDumpTimestampMs  atomic.Int64
	isDumpingActive      atomic.Bool
	droppedSpikeCounter  atomic.Uint64
	totalDumpsCompleted  atomic.Uint64
	stateMutex           sync.Mutex
	isRunning            bool
}

// NewFlightRecorderManager initializes a new manager with standard-library FlightRecorder
func NewFlightRecorderManager(configuration FlightRecorderConfig) *FlightRecorderManager {
	flightRecorderConfig := trace.FlightRecorderConfig{
		MinAge:   configuration.MinEventAge,
		MaxBytes: configuration.MaxRingBufferBytes,
	}

	return &FlightRecorderManager{
		configuration:      configuration,
		runtimeRecorder:    trace.NewFlightRecorder(flightRecorderConfig),
		dumpRequestChannel: make(chan DumpRequest, 4),
	}
}

// Start launches the runtime flight recorder and starts the asynchronous disk snapshot worker
func (manager *FlightRecorderManager) Start(parentContext context.Context) error {
	manager.stateMutex.Lock()
	defer manager.stateMutex.Unlock()

	if manager.isRunning {
		return nil
	}

	if !manager.configuration.Enabled {
		slog.Info("Tracer Flight Recorder is disabled by configuration")
		return nil
	}

	if ensureError := ensureDirectoryExists(manager.configuration.TraceDirectory); ensureError != nil {
		return fmt.Errorf("failed to initialize trace directory %s: %w", manager.configuration.TraceDirectory, ensureError)
	}

	if recorderStartError := manager.runtimeRecorder.Start(); recorderStartError != nil {
		return fmt.Errorf("failed to start Go runtime flight recorder: %w", recorderStartError)
	}

	manager.lifecycleContext, manager.lifecycleCancel = context.WithCancel(parentContext)
	manager.serviceStartedAt = time.Now()
	manager.isRunning = true

	manager.backgroundWaitGroup.Add(1)
	go manager.backgroundSnapshotWorker()

	slog.Info("Tracer Flight Recorder started successfully",
		"trace_directory", manager.configuration.TraceDirectory,
		"max_ring_buffer_bytes", manager.configuration.MaxRingBufferBytes,
		"min_event_age", manager.configuration.MinEventAge.String(),
		"cooldown_duration", manager.configuration.CooldownDuration.String(),
		"startup_grace_period", manager.configuration.StartupGracePeriod.String(),
	)

	return nil
}

// Stop cleanly terminates the flight recorder and waits for any pending snapshot to flush
func (manager *FlightRecorderManager) Stop() error {
	manager.stateMutex.Lock()
	if !manager.isRunning {
		manager.stateMutex.Unlock()
		return nil
	}
	manager.isRunning = false
	manager.lifecycleCancel()
	manager.stateMutex.Unlock()

	// Wait for background worker to exit cleanly
	manager.backgroundWaitGroup.Wait()

	// Stop Go runtime trace flight recording
	manager.runtimeRecorder.Stop()

	slog.Info("Tracer Flight Recorder stopped cleanly",
		"total_dumps_completed", manager.totalDumpsCompleted.Load(),
		"dropped_spikes_count", manager.droppedSpikeCounter.Load(),
	)
	return nil
}

// RecordLatency inspects the measured duration against the threshold and enqueues a non-blocking dump if anomalous
func (manager *FlightRecorderManager) RecordLatency(triggerReason string, measuredDuration, thresholdDuration time.Duration) {
	if manager == nil || !manager.isRunning || !manager.configuration.Enabled || measuredDuration < thresholdDuration {
		return
	}

	// 1. Check startup grace period to suppress false positives during system initialization
	if time.Since(manager.serviceStartedAt) < manager.configuration.StartupGracePeriod {
		return
	}

	// 2. Check cooldown window to prevent cascading dump storms during downstream outages
	lastDumpUnixMs := manager.lastDumpTimestampMs.Load()
	if lastDumpUnixMs > 0 {
		elapsedSinceLastDump := time.Since(time.UnixMilli(lastDumpUnixMs))
		if elapsedSinceLastDump < manager.configuration.CooldownDuration {
			manager.droppedSpikeCounter.Add(1)
			return
		}
	}

	// 3. Prepare dump request payload
	dumpJob := DumpRequest{
		TriggerReason:      triggerReason,
		MeasuredDuration:   measuredDuration,
		ThresholdDuration:  thresholdDuration,
		TriggerSource:      TriggerSourceWatchdog,
		TriggerTimestamp:   time.Now(),
		ResultNotification: nil, // Watchdog is fire-and-forget; does not block
	}

	// 4. Non-blocking channel send (O(1) execution time on trading hot path)
	select {
	case manager.dumpRequestChannel <- dumpJob:
	default:
		// Queue full; dump already in progress or backlog saturated, drop safely
		manager.droppedSpikeCounter.Add(1)
	}
}

// TriggerManualDump initiates an on-demand snapshot from administrative APIs and blocks until completed
func (manager *FlightRecorderManager) TriggerManualDump(callerContext context.Context, triggerReason string) (*TraceMetadata, error) {
	if manager == nil || !manager.isRunning || !manager.configuration.Enabled {
		return nil, errors.New("flight recorder is not active or enabled")
	}

	if triggerReason == "" {
		triggerReason = "manual_admin_dump"
	}

	resultNotificationChannel := make(chan DumpResult, 1)
	dumpJob := DumpRequest{
		TriggerReason:      triggerReason,
		MeasuredDuration:   0,
		ThresholdDuration:  0,
		TriggerSource:      TriggerSourceManualHTTP,
		TriggerTimestamp:   time.Now(),
		ResultNotification: resultNotificationChannel,
	}

	select {
	case manager.dumpRequestChannel <- dumpJob:
	case <-callerContext.Done():
		return nil, callerContext.Err()
	}

	select {
	case result := <-resultNotificationChannel:
		return result.Metadata, result.Error
	case <-callerContext.Done():
		return nil, callerContext.Err()
	}
}

// backgroundSnapshotWorker executes in a separate background goroutine, performing file I/O safely away from trading actors
func (manager *FlightRecorderManager) backgroundSnapshotWorker() {
	defer manager.backgroundWaitGroup.Done()

	for {
		select {
		case <-manager.lifecycleContext.Done():
			// Process any immediately remaining item before shutdown
			select {
			case dumpJob := <-manager.dumpRequestChannel:
				_, _ = manager.executeSnapshot(dumpJob)
			default:
			}
			return

		case dumpJob := <-manager.dumpRequestChannel:
			metadata, executionError := manager.executeSnapshot(dumpJob)
			if dumpJob.ResultNotification != nil {
				dumpJob.ResultNotification <- DumpResult{
					Metadata: metadata,
					Error:    executionError,
				}
			}
		}
	}
}

// executeSnapshot writes the circular ring buffer to disk, creates companion metadata, and maintains file retention
func (manager *FlightRecorderManager) executeSnapshot(dumpJob DumpRequest) (*TraceMetadata, error) {
	if !manager.isDumpingActive.CompareAndSwap(false, true) {
		return nil, errors.New("a trace snapshot is already in progress")
	}
	defer manager.isDumpingActive.Store(false)

	// Clean reason label for safe file naming
	sanitizedReason := reasonSanitizationRegex.ReplaceAllString(dumpJob.TriggerReason, "_")
	sanitizedReason = strings.Trim(sanitizedReason, "_")
	if sanitizedReason == "" {
		sanitizedReason = "trace_snapshot"
	}

	timestampFormatted := dumpJob.TriggerTimestamp.Format("20060102_150405")
	finalTraceFileName := fmt.Sprintf("trace_%s_%s.trace", timestampFormatted, sanitizedReason)
	finalTraceFilePath := filepath.Join(manager.configuration.TraceDirectory, finalTraceFileName)
	temporaryTraceFilePath := finalTraceFilePath + ".tmp"
	companionMetadataPath := filepath.Join(manager.configuration.TraceDirectory, fmt.Sprintf("trace_%s_%s.json", timestampFormatted, sanitizedReason))

	// Annotate the Go execution trace with a dedicated user event marker for quick timeline identification
	trace.Log(context.Background(), "flight_recorder_snapshot", fmt.Sprintf("reason=%s duration_ms=%.2f threshold_ms=%.2f",
		dumpJob.TriggerReason,
		float64(dumpJob.MeasuredDuration.Microseconds())/1000.0,
		float64(dumpJob.ThresholdDuration.Microseconds())/1000.0,
	))

	// Create temporary trace file
	temporaryFile, creationError := os.OpenFile(temporaryTraceFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if creationError != nil {
		slog.Error("Failed to create temporary trace file", "path", temporaryTraceFilePath, "error", creationError)
		return nil, creationError
	}

	bytesWritten, writeError := manager.runtimeRecorder.WriteTo(temporaryFile)
	closeError := temporaryFile.Close()

	if writeError != nil {
		_ = os.Remove(temporaryTraceFilePath)
		slog.Error("Failed to write flight recorder snapshot to file", "error", writeError)
		return nil, writeError
	}
	if closeError != nil {
		_ = os.Remove(temporaryTraceFilePath)
		return nil, closeError
	}

	// Atomically rename temporary trace file to final destination
	if renameError := os.Rename(temporaryTraceFilePath, finalTraceFilePath); renameError != nil {
		_ = os.Remove(temporaryTraceFilePath)
		return nil, renameError
	}

	// Capture runtime performance telemetry at the instant of the dump
	var memoryStatistics runtime.MemStats
	runtime.ReadMemStats(&memoryStatistics)

	traceMetadata := &TraceMetadata{
		TraceFileName:        finalTraceFileName,
		SnapshotTimestamp:    dumpJob.TriggerTimestamp,
		TriggerReason:        dumpJob.TriggerReason,
		MeasuredLatencyMs:    float64(dumpJob.MeasuredDuration.Microseconds()) / 1000.0,
		ThresholdLatencyMs:   float64(dumpJob.ThresholdDuration.Microseconds()) / 1000.0,
		TriggerSource:        dumpJob.TriggerSource,
		TraceFileSizeBytes:   bytesWritten,
		ActiveGoroutineCount: runtime.NumGoroutine(),
		AllocatedHeapBytes:   memoryStatistics.Alloc,
		TotalAllocatedBytes:  memoryStatistics.TotalAlloc,
		CumulativeGCCycles:   memoryStatistics.NumGC,
		DroppedSpikeCount:    manager.droppedSpikeCounter.Load(),
		TotalDumpsCompleted:  manager.totalDumpsCompleted.Load() + 1,
	}

	// Write companion JSON metadata alongside the binary trace file
	if metadataWriteError := writeCompanionMetadata(companionMetadataPath, traceMetadata); metadataWriteError != nil {
		slog.Warn("Failed to write companion metadata file", "path", companionMetadataPath, "error", metadataWriteError)
	}

	// Update operational state counters
	manager.lastDumpTimestampMs.Store(dumpJob.TriggerTimestamp.UnixMilli())
	manager.totalDumpsCompleted.Add(1)

	// Clean up older traces exceeding retention limits
	if pruneError := pruneRetainedTraces(manager.configuration.TraceDirectory, manager.configuration.MaxRetainedTraceFiles, manager.configuration.MaxRetainedTotalBytes); pruneError != nil {
		slog.Warn("Failed to prune old trace snapshots", "error", pruneError)
	}

	slog.Info("Trace snapshot successfully saved",
		"file", finalTraceFileName,
		"size_bytes", bytesWritten,
		"reason", dumpJob.TriggerReason,
		"measured_latency_ms", traceMetadata.MeasuredLatencyMs,
		"source", dumpJob.TriggerSource,
	)

	return traceMetadata, nil
}

// GetStatus returns the current operational health and metrics of the FlightRecorder
func (manager *FlightRecorderManager) GetStatus() FlightRecorderStatus {
	if manager == nil {
		return FlightRecorderStatus{IsEnabled: false, IsRunning: false}
	}

	summaries, totalBytes, _ := listTraceSummaries(manager.configuration.TraceDirectory)
	var lastDumpTime time.Time
	lastDumpMs := manager.lastDumpTimestampMs.Load()
	if lastDumpMs > 0 {
		lastDumpTime = time.UnixMilli(lastDumpMs)
	}

	return FlightRecorderStatus{
		IsEnabled:               manager.configuration.Enabled,
		IsRunning:               manager.isRunning,
		IsCurrentlyDumping:      manager.isDumpingActive.Load(),
		StartedAt:               manager.serviceStartedAt,
		LastDumpAt:              lastDumpTime,
		TotalDumpsCompleted:     manager.totalDumpsCompleted.Load(),
		DroppedSpikeCount:       manager.droppedSpikeCounter.Load(),
		RetainedTraceFilesCount: len(summaries),
		RetainedTotalSizeBytes:  totalBytes,
	}
}

