package diagnostics

import (
	"time"
)

// FlightRecorderConfig holds the operational parameters for the runtime execution trace flight recorder
type FlightRecorderConfig struct {
	Enabled               bool          `json:"enabled"`
	TraceDirectory        string        `json:"trace_directory"`
	MaxRingBufferBytes    uint64        `json:"max_ring_buffer_bytes"`
	MinEventAge           time.Duration `json:"min_event_age"`
	CooldownDuration      time.Duration `json:"cooldown_duration"`
	StartupGracePeriod    time.Duration `json:"startup_grace_period"`
	MaxRetainedTraceFiles int           `json:"max_retained_trace_files"`
	MaxRetainedTotalBytes int64         `json:"max_retained_total_bytes"`
}

// DefaultFlightRecorderConfig provides safe production defaults with bounded memory and disk usage
func DefaultFlightRecorderConfig() FlightRecorderConfig {
	return FlightRecorderConfig{
		Enabled:               true,
		TraceDirectory:        "data/traces",
		MaxRingBufferBytes:    8 * 1024 * 1024,  // 8 MB ring buffer
		MinEventAge:           10 * time.Second, // Retain at least 10 seconds of runtime events
		CooldownDuration:      60 * time.Second, // Minimum 60s cooldown between automatic dumps
		StartupGracePeriod:    20 * time.Second, // Ignore startup warmup spikes during first 20s
		MaxRetainedTraceFiles: 10,               // Keep at most 10 recent trace snapshots
		MaxRetainedTotalBytes: 100 * 1024 * 1024, // 100 MB maximum disk retention
	}
}

// DumpTriggerSource identifies what initiated the trace snapshot
type DumpTriggerSource string

const (
	TriggerSourceWatchdog   DumpTriggerSource = "watchdog"
	TriggerSourceManualHTTP DumpTriggerSource = "manual_http"
)

// DumpRequest represents a scheduled snapshot job dispatched to the background worker
type DumpRequest struct {
	TriggerReason      string
	MeasuredDuration   time.Duration
	ThresholdDuration  time.Duration
	TriggerSource      DumpTriggerSource
	TriggerTimestamp   time.Time
	ResultNotification chan DumpResult
}

// DumpResult returns the completed snapshot metadata or any encounter error to synchronous callers
type DumpResult struct {
	Metadata *TraceMetadata
	Error    error
}

// TraceMetadata contains comprehensive runtime metrics and diagnostic annotations saved alongside the trace file
type TraceMetadata struct {
	TraceFileName         string            `json:"trace_file_name"`
	SnapshotTimestamp     time.Time         `json:"snapshot_timestamp"`
	TriggerReason         string            `json:"trigger_reason"`
	MeasuredLatencyMs     float64           `json:"measured_latency_ms"`
	ThresholdLatencyMs    float64           `json:"threshold_latency_ms"`
	TriggerSource         DumpTriggerSource `json:"trigger_source"`
	TraceFileSizeBytes    int64             `json:"trace_file_size_bytes"`
	ActiveGoroutineCount  int               `json:"active_goroutine_count"`
	AllocatedHeapBytes    uint64            `json:"allocated_heap_bytes"`
	TotalAllocatedBytes   uint64            `json:"total_allocated_bytes"`
	CumulativeGCCycles    uint32            `json:"cumulative_gc_cycles"`
	DroppedSpikeCount     uint64            `json:"dropped_spike_count"`
	TotalDumpsCompleted   uint64            `json:"total_dumps_completed"`
	CustomAnnotations     map[string]string `json:"custom_annotations,omitempty"`
}

// TraceSummaryItem represents a brief descriptor of an existing trace snapshot on disk
type TraceSummaryItem struct {
	FileName           string    `json:"file_name"`
	FileSizeBytes      int64     `json:"file_size_bytes"`
	LastModifiedTime   time.Time `json:"last_modified_time"`
	TriggerReason      string    `json:"trigger_reason,omitempty"`
	MeasuredLatencyMs  float64   `json:"measured_latency_ms,omitempty"`
	ThresholdLatencyMs float64   `json:"threshold_latency_ms,omitempty"`
}

// FlightRecorderStatus describes the current operational health and metrics of the recorder
type FlightRecorderStatus struct {
	IsEnabled               bool      `json:"is_enabled"`
	IsRunning               bool      `json:"is_running"`
	IsCurrentlyDumping      bool      `json:"is_currently_dumping"`
	StartedAt               time.Time `json:"started_at"`
	LastDumpAt              time.Time `json:"last_dump_at,omitempty"`
	TotalDumpsCompleted     uint64    `json:"total_dumps_completed"`
	DroppedSpikeCount       uint64    `json:"dropped_spike_count"`
	RetainedTraceFilesCount int       `json:"retained_trace_files_count"`
	RetainedTotalSizeBytes  int64     `json:"retained_total_size_bytes"`
}

