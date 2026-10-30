package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	errInvalidFileName   = errors.New("invalid or unsafe trace file name")
	errTraceFileNotFound = errors.New("trace file not found")
)

// ensureDirectoryExists checks if target directory exists and creates it with restricted permissions if not
func ensureDirectoryExists(directoryPath string) error {
	if directoryPath == "" {
		return errors.New("directory path cannot be empty")
	}
	return os.MkdirAll(directoryPath, 0750)
}

// writeCompanionMetadata saves diagnostic context in JSON format beside the binary trace file
func writeCompanionMetadata(metadataFilePath string, metadata *TraceMetadata) error {
	serializedBytes, serializationError := json.MarshalIndent(metadata, "", "  ")
	if serializationError != nil {
		return fmt.Errorf("failed to serialize companion trace metadata: %w", serializationError)
	}

	temporaryMetadataPath := metadataFilePath + ".tmp"
	if writeError := os.WriteFile(temporaryMetadataPath, serializedBytes, 0640); writeError != nil {
		return fmt.Errorf("failed to write temporary companion metadata: %w", writeError)
	}

	if renameError := os.Rename(temporaryMetadataPath, metadataFilePath); renameError != nil {
		_ = os.Remove(temporaryMetadataPath)
		return fmt.Errorf("failed to atomically rename companion metadata: %w", renameError)
	}

	return nil
}

// readCompanionMetadata reads the JSON companion metadata file if present
func readCompanionMetadata(metadataFilePath string) (*TraceMetadata, error) {
	fileBytes, readError := os.ReadFile(metadataFilePath)
	if readError != nil {
		return nil, readError
	}

	var metadata TraceMetadata
	if unmarshalError := json.Unmarshal(fileBytes, &metadata); unmarshalError != nil {
		return nil, unmarshalError
	}

	return &metadata, nil
}

type fileCandidate struct {
	fullPath         string
	metadataPath     string
	fileName         string
	fileSizeBytes    int64
	lastModifiedTime time.Time
}

// pruneRetainedTraces scans the trace directory and cleans up the oldest trace files exceeding configured limits
func pruneRetainedTraces(directoryPath string, maxRetainedFiles int, maxRetainedTotalBytes int64) error {
	if maxRetainedFiles <= 0 {
		maxRetainedFiles = 10
	}
	if maxRetainedTotalBytes <= 0 {
		maxRetainedTotalBytes = 100 * 1024 * 1024
	}

	directoryEntries, readDirectoryError := os.ReadDir(directoryPath)
	if readDirectoryError != nil {
		if errors.Is(readDirectoryError, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read trace directory for retention pruning: %w", readDirectoryError)
	}

	var candidates []fileCandidate
	var totalAccumulatedBytes int64

	for _, entry := range directoryEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".trace") {
			continue
		}

		entryFileInfo, statError := entry.Info()
		if statError != nil {
			continue
		}

		fullFilePath := filepath.Join(directoryPath, entry.Name())
		companionMetadataPath := strings.TrimSuffix(fullFilePath, ".trace") + ".json"

		candidates = append(candidates, fileCandidate{
			fullPath:         fullFilePath,
			metadataPath:     companionMetadataPath,
			fileName:         entry.Name(),
			fileSizeBytes:    entryFileInfo.Size(),
			lastModifiedTime: entryFileInfo.ModTime(),
		})
		totalAccumulatedBytes += entryFileInfo.Size()
	}

	// Sort candidates ascending by modification time (oldest files first)
	sort.Slice(candidates, func(firstIndex, secondIndex int) bool {
		return candidates[firstIndex].lastModifiedTime.Before(candidates[secondIndex].lastModifiedTime)
	})

	filesToDeleteCount := len(candidates) - maxRetainedFiles
	if filesToDeleteCount < 0 {
		filesToDeleteCount = 0
	}

	for candidateIndex := 0; candidateIndex < len(candidates); candidateIndex++ {
		targetCandidate := candidates[candidateIndex]
		shouldDeleteDueToFileCount := candidateIndex < filesToDeleteCount
		shouldDeleteDueToByteQuota := totalAccumulatedBytes > maxRetainedTotalBytes

		if shouldDeleteDueToFileCount || shouldDeleteDueToByteQuota {
			if removalError := os.Remove(targetCandidate.fullPath); removalError == nil {
				totalAccumulatedBytes -= targetCandidate.fileSizeBytes
				slog.Info("Pruned aged trace file to maintain retention limits", "file", targetCandidate.fileName, "size_bytes", targetCandidate.fileSizeBytes)
			}
			// Attempt to remove companion JSON metadata file if it exists
			_ = os.Remove(targetCandidate.metadataPath)
		}
	}

	return nil
}

// listTraceSummaries inspects the trace directory and produces structured summaries of all saved trace files
func listTraceSummaries(directoryPath string) ([]TraceSummaryItem, int64, error) {
	directoryEntries, readDirectoryError := os.ReadDir(directoryPath)
	if readDirectoryError != nil {
		if errors.Is(readDirectoryError, os.ErrNotExist) {
			return []TraceSummaryItem{}, 0, nil
		}
		return nil, 0, readDirectoryError
	}

	var summaries []TraceSummaryItem
	var totalRetainedBytes int64

	for _, entry := range directoryEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".trace") {
			continue
		}

		entryFileInfo, statError := entry.Info()
		if statError != nil {
			continue
		}

		totalRetainedBytes += entryFileInfo.Size()
		summaryItem := TraceSummaryItem{
			FileName:         entry.Name(),
			FileSizeBytes:    entryFileInfo.Size(),
			LastModifiedTime: entryFileInfo.ModTime(),
		}

		// Read companion metadata to enrich summary if available
		companionMetadataPath := filepath.Join(directoryPath, strings.TrimSuffix(entry.Name(), ".trace")+".json")
		if metadata, readError := readCompanionMetadata(companionMetadataPath); readError == nil && metadata != nil {
			summaryItem.TriggerReason = metadata.TriggerReason
			summaryItem.MeasuredLatencyMs = metadata.MeasuredLatencyMs
			summaryItem.ThresholdLatencyMs = metadata.ThresholdLatencyMs
		}

		summaries = append(summaries, summaryItem)
	}

	// Sort descending by modification time (most recent trace first)
	sort.Slice(summaries, func(firstIndex, secondIndex int) bool {
		return summaries[firstIndex].LastModifiedTime.After(summaries[secondIndex].LastModifiedTime)
	})

	return summaries, totalRetainedBytes, nil
}

// resolveSafeTraceFilePath verifies that the requested trace file name is clean, strictly ends with .trace, and resides within the base directory
func resolveSafeTraceFilePath(baseDirectoryPath, requestedFileName string) (string, error) {
	sanitizedBaseName := filepath.Base(requestedFileName)
	if sanitizedBaseName != requestedFileName || !strings.HasSuffix(sanitizedBaseName, ".trace") {
		return "", errInvalidFileName
	}

	resolvedFullPath := filepath.Join(baseDirectoryPath, sanitizedBaseName)
	cleanBasePath := filepath.Clean(baseDirectoryPath)
	cleanResolvedPath := filepath.Clean(resolvedFullPath)

	if !strings.HasPrefix(cleanResolvedPath, cleanBasePath) {
		return "", errInvalidFileName
	}

	fileInformation, statError := os.Stat(cleanResolvedPath)
	if statError != nil {
		if errors.Is(statError, os.ErrNotExist) {
			return "", errTraceFileNotFound
		}
		return "", statError
	}

	if fileInformation.IsDir() {
		return "", errInvalidFileName
	}

	return cleanResolvedPath, nil
}

