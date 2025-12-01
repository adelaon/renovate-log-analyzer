// Copyright 2025 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package doctor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Renovate's numerical levels to standard string names
var renovateLogLevels = map[int]string{
	10: "TRACE",
	20: "DEBUG",
	30: "INFO",
	40: "WARN",
	50: "ERROR",
	60: "FATAL",
}

// ProcessLogFile processes logs from a file instead of streaming
func ProcessLogFile(ctx context.Context, logFilePath string) (string, *SimpleReport, error) {
	errorsMap := make(map[string]int)
	fatalMap := make(map[string]int)
	report := &SimpleReport{}

	// Check if file exists
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		return "", report, fmt.Errorf("log file not found (step-renovate may not have run), path: %s", logFilePath)
	}

	// Open and read the file
	file, err := os.Open(logFilePath)
	if err != nil {
		return "", report, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Read line by line
	const maxBufferSize = 1 * 1024 * 1024
	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxBufferSize)
	scanner.Buffer(buf, maxBufferSize)

	lineCount := 0

	for scanner.Scan() {
		// Check cancellation every 100 lines to reduce overhead
		if lineCount%100 == 0 {
			select {
			case <-ctx.Done():
				if len(errorsMap) == 0 && len(fatalMap) == 0 && len(report.Errors) == 0 && len(report.Warnings) == 0 && len(report.Infos) == 0 {
					return "", report, fmt.Errorf("log processing cancelled: %w", ctx.Err())
				}
				return buildErrorMessageFromLogs(errorsMap, fatalMap), report, nil
			default:
			}
		}
		lineCount++
		line := scanner.Text()

		// Attempt to parse the JSON log line
		entry, err := parseLogLine(line)
		if err != nil {
			continue
		}

		switch entry.Level {
		case "FATAL":
			formattedErr := buildErrorMessage(entry)
			fatalMap[formattedErr]++
		case "ERROR":
			formattedErr := buildErrorMessage(entry)
			errorsMap[formattedErr]++
		}

		// Check against registered selectors
		for selector, checkFunc := range Selectors {
			if strings.Contains(entry.Msg, selector) {
				checkFunc(&entry, report)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if len(errorsMap) == 0 && len(fatalMap) == 0 && len(report.Errors) == 0 && len(report.Warnings) == 0 && len(report.Infos) == 0 {
			return "", report, fmt.Errorf("error reading log file: %w", err)
		}
		return buildErrorMessageFromLogs(errorsMap, fatalMap), report, nil
	}

	return buildErrorMessageFromLogs(errorsMap, fatalMap), report, nil
}

// unmarshal the JSON log line and extract important fields
func parseLogLine(line string) (LogEntry, error) {
	var rawData map[string]any
	if err := json.Unmarshal([]byte(line), &rawData); err != nil {
		return LogEntry{}, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	// assign known fields to the final structure
	entry := LogEntry{
		Extras: make(map[string]any),
	}

	// extract standard fields, converting types as needed
	for k, v := range rawData {
		switch k {
		// extract known Renovate log levels
		case "level":
			levelFloat, ok := v.(float64)
			if !ok {
				continue
			}

			levelInt := int(levelFloat)
			levelStr, found := renovateLogLevels[levelInt]
			if !found {
				continue
			}

			entry.Level = levelStr
		// keep a valid string log message
		case "msg":
			msgStr, ok := v.(string)
			if !ok {
				continue
			}

			entry.Msg = msgStr
		// keep only relevant extra fields
		case "err", "errors", "errorMessage", "branch", "durationMs", "depName",
			"branchesInformation", "context", "packageFile", "currentValue",
			"previousNewValue", "thisNewValue", "oldConfig", "newConfig", "migratedConfig":
			entry.Extras[k] = v
		}
	}
	return entry, nil
}

// process structured logs to find errors/fatals and build a summary message
func buildErrorMessageFromLogs(errorsMap, fatalMap map[string]int) string {
	errString := formatFailMsg(errorsMap, "ERROR")
	fatalString := formatFailMsg(fatalMap, "FATAL")

	if errString == "" && fatalString == "" {
		return ""
	}

	return fmt.Sprintf("Mintmaker finished with %s%s",
		errString,
		fatalString)
}

// create summary with counts for duplicates
func formatFailMsg(logs map[string]int, logLevel string) string {
	if len(logs) == 0 {
		return ""
	}

	totalCount := 0
	var uniqueMessages []string

	for msg, count := range logs {
		totalCount += count

		if count > 1 {
			uniqueMessages = append(uniqueMessages, fmt.Sprintf("%dx %s", count, msg))
		} else {
			uniqueMessages = append(uniqueMessages, msg)
		}
	}

	return fmt.Sprintf("%d %s: %s", totalCount, logLevel, strings.Join(uniqueMessages, ""))
}

// build a single error message from a log entry, including nested error details if available
func buildErrorMessage(logEntry LogEntry) string {
	errMsg := logEntry.Msg

	// Try to get additional error details
	if errMap, ok := logEntry.Extras["err"].(map[string]any); ok {
		if message, ok := errMap["message"].(string); ok {
			return fmt.Sprintf("%s: %s", errMsg, message)
		}
	}

	if errorMessage, ok := logEntry.Extras["errorMessage"].(string); ok {
		return fmt.Sprintf("%s: %s", errMsg, errorMessage)
	}

	return fmt.Sprintf("%s\n", errMsg)
}
