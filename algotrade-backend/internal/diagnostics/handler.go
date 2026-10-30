package diagnostics

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ajaynegi33/AlgoTrade/internal/response"
)

// Handler serves HTTP endpoints for runtime diagnostics and execution trace inspection
type Handler struct {
	flightRecorderManager *FlightRecorderManager
}

// NewHandler constructs a diagnostics HTTP handler
func NewHandler(flightRecorderManager *FlightRecorderManager) *Handler {
	return &Handler{
		flightRecorderManager: flightRecorderManager,
	}
}

// TriggerManualDump executes an on-demand snapshot of the circular trace buffer
func (handler *Handler) TriggerManualDump(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	if handler.flightRecorderManager == nil {
		response.Error(responseWriter, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Flight recorder is not initialized")
		return
	}

	triggerReason := httpRequest.URL.Query().Get("reason")
	if httpRequest.Body != nil && httpRequest.ContentLength > 0 {
		var requestBody struct {
			Reason string `json:"reason"`
		}
		if decodeError := json.NewDecoder(httpRequest.Body).Decode(&requestBody); decodeError == nil && requestBody.Reason != "" {
			triggerReason = requestBody.Reason
		}
	}

	if triggerReason == "" {
		triggerReason = "manual_api_trigger"
	}

	traceMetadata, dumpError := handler.flightRecorderManager.TriggerManualDump(httpRequest.Context(), triggerReason)
	if dumpError != nil {
		response.Error(responseWriter, http.StatusInternalServerError, "TRACE_DUMP_FAILED", dumpError.Error())
		return
	}

	response.Success(responseWriter, http.StatusOK, traceMetadata)
}

// GetStatus returns the operational health and trace buffer statistics
func (handler *Handler) GetStatus(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	if handler.flightRecorderManager == nil {
		response.Error(responseWriter, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Flight recorder is not initialized")
		return
	}

	operationalStatus := handler.flightRecorderManager.GetStatus()
	response.Success(responseWriter, http.StatusOK, operationalStatus)
}

// ListTraces returns summary descriptors of all trace files currently stored on disk
func (handler *Handler) ListTraces(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	if handler.flightRecorderManager == nil {
		response.Error(responseWriter, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Flight recorder is not initialized")
		return
	}

	traceDirectory := handler.flightRecorderManager.configuration.TraceDirectory
	summaries, totalRetainedBytes, listError := listTraceSummaries(traceDirectory)
	if listError != nil {
		response.Error(responseWriter, http.StatusInternalServerError, "LIST_TRACES_FAILED", listError.Error())
		return
	}

	response.Success(responseWriter, http.StatusOK, map[string]any{
		"traces":               summaries,
		"retained_files_count": len(summaries),
		"retained_total_bytes": totalRetainedBytes,
	})
}

// DownloadTrace streams a specific .trace binary file to the caller for inspection with 'go tool trace'
func (handler *Handler) DownloadTrace(responseWriter http.ResponseWriter, httpRequest *http.Request) {
	if handler.flightRecorderManager == nil {
		response.Error(responseWriter, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Flight recorder is not initialized")
		return
	}

	// Extract filename from URL path: /api/diagnostics/flight-recorder/traces/{filename}
	requestedFileName := httpRequest.PathValue("filename")
	if requestedFileName == "" {
		pathSegments := strings.Split(httpRequest.URL.Path, "/")
		if len(pathSegments) > 0 {
			requestedFileName = pathSegments[len(pathSegments)-1]
		}
	}

	traceDirectory := handler.flightRecorderManager.configuration.TraceDirectory
	safeFilePath, resolveError := resolveSafeTraceFilePath(traceDirectory, requestedFileName)
	if resolveError != nil {
		if resolveError == errTraceFileNotFound {
			response.Error(responseWriter, http.StatusNotFound, "NOT_FOUND", "Requested trace file does not exist")
			return
		}
		response.Error(responseWriter, http.StatusBadRequest, "INVALID_FILE_NAME", "Invalid or unsafe trace file name")
		return
	}

	responseWriter.Header().Set("Content-Type", "application/octet-stream")
	responseWriter.Header().Set("Content-Disposition", "attachment; filename=\""+requestedFileName+"\"")
	http.ServeFile(responseWriter, httpRequest, safeFilePath)
}

