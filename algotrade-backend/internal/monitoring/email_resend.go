package monitoring

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// ResendEmailPayload represents the JSON payload expected by api.resend.com/emails
type ResendEmailPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// ResendEmailResponse represents the success or error payload returned by Resend
type ResendEmailResponse struct {
	ID         string `json:"id,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`
	Name       string `json:"name,omitempty"`
}

// SendEODReportEmail transmits the generated HTML diagnostic report via the Resend API
func SendEODReportEmail(ctx context.Context, db *sql.DB, apiKey, toEmail, tradingDate, htmlContent string) error {
	if apiKey == "" {
		apiKey = os.Getenv("RESEND_API_KEY")
	}
	if toEmail == "" {
		toEmail = os.Getenv("REPORT_EMAIL")
		if toEmail == "" {
			toEmail = os.Getenv("reportemail")
		}
	}

	if apiKey == "" {
		slog.Warn("RESEND_API_KEY is not set; skipping email dispatch", "date", tradingDate)
		return nil
	}
	if toEmail == "" {
		slog.Warn("REPORT_EMAIL is not set; skipping email dispatch", "date", tradingDate)
		return nil
	}

	fromEmail := os.Getenv("RESEND_FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = "AlgoTrade Desk <onboarding@resend.dev>"
	}

	payload := ResendEmailPayload{
		From:    fromEmail,
		To:      []string{strings.TrimSpace(toEmail)},
		Subject: fmt.Sprintf("AlgoTrade EOD Diagnostic Report — %s", tradingDate),
		HTML:    htmlContent,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Resend email payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create Resend HTTP request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "AlgoTrade-Execution-Engine/2.0")

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request to Resend API: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend API returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	var resendResp ResendEmailResponse
	_ = json.Unmarshal(respBody, &resendResp)

	slog.Info("Successfully dispatched EOD diagnostic report via Resend",
		"date", tradingDate,
		"recipient", toEmail,
		"email_id", resendResp.ID,
	)

	if db != nil {
		_, _ = db.Exec(`
			UPDATE daily_eod_reports
			SET is_email_sent = 1, email_recipient = ?
			WHERE trading_date = ?
		`, toEmail, tradingDate)
	}

	return nil
}

