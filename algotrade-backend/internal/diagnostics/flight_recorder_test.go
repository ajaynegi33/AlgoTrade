package diagnostics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFlightRecorderLifecycleAndDump(t *testing.T) {
	temporaryDirectory := t.TempDir()

	testConfiguration := FlightRecorderConfig{
		Enabled:               true,
		TraceDirectory:        temporaryDirectory,
		MaxRingBufferBytes:    2 * 1024 * 1024,
		MinEventAge:           1 * time.Second,
		CooldownDuration:      100 * time.Millisecond,
		StartupGracePeriod:    0, // Disable grace period for unit testing
		MaxRetainedTraceFiles: 5,
		MaxRetainedTotalBytes: 50 * 1024 * 1024,
	}

	flightRecorderManager := NewFlightRecorderManager(testConfiguration)
	parentContext, cancelParentContext := context.WithCancel(context.Background())
	defer cancelParentContext()

	if startError := flightRecorderManager.Start(parentContext); startError != nil {
		t.Fatalf("failed to start flight recorder: %v", startError)
	}
	defer func() {
		_ = flightRecorderManager.Stop()
	}()

	// 1. Verify status while running
	initialStatus := flightRecorderManager.GetStatus()
	if !initialStatus.IsRunning || !initialStatus.IsEnabled {
		t.Fatalf("expected flight recorder to be running and enabled, got %+v", initialStatus)
	}

	// 2. Below threshold latency should NOT trigger a dump
	flightRecorderManager.RecordLatency("test_fast_path", 5*time.Millisecond, 50*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	summariesAfterBelowThreshold, _, _ := listTraceSummaries(temporaryDirectory)
	if len(summariesAfterBelowThreshold) != 0 {
		t.Fatalf("expected 0 traces for below-threshold latency, got %d", len(summariesAfterBelowThreshold))
	}

	// 3. Above threshold latency SHOULD trigger a snapshot
	flightRecorderManager.RecordLatency("test_spike_anomaly", 120*time.Millisecond, 50*time.Millisecond)

	// Wait briefly for background snapshot worker to write file
	deadline := time.Now().Add(3 * time.Second)
	var foundTraces []TraceSummaryItem
	for time.Now().Before(deadline) {
		summaries, _, listError := listTraceSummaries(temporaryDirectory)
		if listError == nil && len(summaries) > 0 {
			foundTraces = summaries
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(foundTraces) == 0 {
		t.Fatalf("expected at least 1 trace snapshot to be written to disk, found 0")
	}

	savedSnapshot := foundTraces[0]
	if savedSnapshot.FileSizeBytes <= 0 {
		t.Fatalf("expected positive trace file size, got %d", savedSnapshot.FileSizeBytes)
	}

	// 4. Verify companion JSON metadata file exists and contains correct information
	expectedMetadataPath := filepath.Join(temporaryDirectory, strings.TrimSuffix(savedSnapshot.FileName, ".trace")+".json")
	metadataBytes, readMetadataError := os.ReadFile(expectedMetadataPath)
	if readMetadataError != nil {
		t.Fatalf("expected companion metadata file at %s: %v", expectedMetadataPath, readMetadataError)
	}

	var parsedMetadata TraceMetadata
	if unmarshalError := json.Unmarshal(metadataBytes, &parsedMetadata); unmarshalError != nil {
		t.Fatalf("failed to unmarshal companion metadata: %v", unmarshalError)
	}

	if parsedMetadata.TriggerReason != "test_spike_anomaly" {
		t.Errorf("expected TriggerReason 'test_spike_anomaly', got '%s'", parsedMetadata.TriggerReason)
	}
	if parsedMetadata.MeasuredLatencyMs < 100.0 {
		t.Errorf("expected MeasuredLatencyMs >= 100.0, got %f", parsedMetadata.MeasuredLatencyMs)
	}

	// 5. Test Cooldown / Debounce: another spike immediately within 100ms cooldown should be dropped
	flightRecorderManager.RecordLatency("test_spike_cooldown", 150*time.Millisecond, 50*time.Millisecond)
	time.Sleep(40 * time.Millisecond)

	statusAfterCooldownTest := flightRecorderManager.GetStatus()
	if statusAfterCooldownTest.DroppedSpikeCount == 0 {
		t.Errorf("expected dropped spikes counter to increment during cooldown, got %d", statusAfterCooldownTest.DroppedSpikeCount)
	}
}

func TestRetentionPruning(t *testing.T) {
	temporaryDirectory := t.TempDir()

	// Create 8 dummy trace files with varying timestamps
	baseTime := time.Now().Add(-10 * time.Hour)
	for fileIndex := 1; fileIndex <= 8; fileIndex++ {
		dummyFileName := filepath.Join(temporaryDirectory, strings.ToLower(string(rune('a'+fileIndex-1)))) + "_trace.trace"
		_ = os.WriteFile(dummyFileName, []byte("mock trace payload"), 0640)
		dummyMetadataName := strings.TrimSuffix(dummyFileName, ".trace") + ".json"
		_ = os.WriteFile(dummyMetadataName, []byte(`{"reason":"test"}`), 0640)

		fileTime := baseTime.Add(time.Duration(fileIndex) * time.Hour)
		_ = os.Chtimes(dummyFileName, fileTime, fileTime)
		_ = os.Chtimes(dummyMetadataName, fileTime, fileTime)
	}

	// Prune to maximum of 4 files
	maxRetainedFiles := 4
	if pruneError := pruneRetainedTraces(temporaryDirectory, maxRetainedFiles, 100*1024*1024); pruneError != nil {
		t.Fatalf("pruneRetainedTraces failed: %v", pruneError)
	}

	remainingSummaries, _, _ := listTraceSummaries(temporaryDirectory)
	if len(remainingSummaries) != maxRetainedFiles {
		t.Fatalf("expected exactly %d remaining traces, got %d", maxRetainedFiles, len(remainingSummaries))
	}
}

func TestDiagnosticsHandlerHTTP(t *testing.T) {
	temporaryDirectory := t.TempDir()

	testConfiguration := FlightRecorderConfig{
		Enabled:               true,
		TraceDirectory:        temporaryDirectory,
		MaxRingBufferBytes:    2 * 1024 * 1024,
		MinEventAge:           1 * time.Second,
		CooldownDuration:      10 * time.Millisecond,
		StartupGracePeriod:    0,
		MaxRetainedTraceFiles: 5,
		MaxRetainedTotalBytes: 50 * 1024 * 1024,
	}

	flightRecorderManager := NewFlightRecorderManager(testConfiguration)
	if startError := flightRecorderManager.Start(context.Background()); startError != nil {
		t.Fatalf("failed to start flight recorder: %v", startError)
	}
	defer func() {
		_ = flightRecorderManager.Stop()
	}()

	diagnosticsHandler := NewHandler(flightRecorderManager)

	// 1. Test GET /status
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/diagnostics/flight-recorder/status", nil)
	statusResponseRecorder := httptest.NewRecorder()
	diagnosticsHandler.GetStatus(statusResponseRecorder, statusRequest)

	if statusResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", statusResponseRecorder.Code)
	}

	// 2. Test POST /dump (Manual on-demand trigger)
	manualDumpRequest := httptest.NewRequest(http.MethodPost, "/api/diagnostics/flight-recorder/dump?reason=manual_admin_test", nil)
	manualDumpResponseRecorder := httptest.NewRecorder()
	diagnosticsHandler.TriggerManualDump(manualDumpResponseRecorder, manualDumpRequest)

	if manualDumpResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for manual dump, got %d body: %s", manualDumpResponseRecorder.Code, manualDumpResponseRecorder.Body.String())
	}

	// 3. Test GET /traces
	listRequest := httptest.NewRequest(http.MethodGet, "/api/diagnostics/flight-recorder/traces", nil)
	listResponseRecorder := httptest.NewRecorder()
	diagnosticsHandler.ListTraces(listResponseRecorder, listRequest)

	if listResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for list traces, got %d", listResponseRecorder.Code)
	}

	// 4. Test safe path resolution & download
	summaries, _, _ := listTraceSummaries(temporaryDirectory)
	if len(summaries) == 0 {
		t.Fatalf("expected at least 1 trace to download")
	}

	validTraceFileName := summaries[0].FileName
	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/diagnostics/flight-recorder/traces/"+validTraceFileName, nil)
	downloadRequest.SetPathValue("filename", validTraceFileName)
	downloadResponseRecorder := httptest.NewRecorder()
	diagnosticsHandler.DownloadTrace(downloadResponseRecorder, downloadRequest)

	if downloadResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK for download, got %d", downloadResponseRecorder.Code)
	}
	if downloadResponseRecorder.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("expected application/octet-stream, got %s", downloadResponseRecorder.Header().Get("Content-Type"))
	}

	// 5. Test directory traversal attack prevention
	unsafeRequest := httptest.NewRequest(http.MethodGet, "/api/diagnostics/flight-recorder/traces/../../etc/passwd", nil)
	unsafeRequest.SetPathValue("filename", "../../etc/passwd")
	unsafeResponseRecorder := httptest.NewRecorder()
	diagnosticsHandler.DownloadTrace(unsafeResponseRecorder, unsafeRequest)

	if unsafeResponseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unsafe filename, got %d", unsafeResponseRecorder.Code)
	}
}

