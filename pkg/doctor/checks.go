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
	"fmt"
	"regexp"
	"strings"
)

// CheckFunc is a function that performs a check on a log line
type CheckFunc func(line *LogEntry, report *SimpleReport)

// Selectors stores all registered selector patterns and their associated check functions
var Selectors = make(map[string]CheckFunc)

// CriticalPatterns contains compiled regex patterns for identifying critical error lines
var criticalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\s*Command failed:`),
	regexp.MustCompile(`(?i)^\s*(Error|FATAL|CRITICAL)\b`),
	regexp.MustCompile(`(?i)^\s*Caused by:`),
	regexp.MustCompile(`(?i)^\s*[\w.]+Error:`),
	regexp.MustCompile(`(?i)permission denied`),
	regexp.MustCompile(`(?i)failed`),
	regexp.MustCompile(`(?i)exception`),
	regexp.MustCompile(`(?i)could not connect`),
	regexp.MustCompile(`(?i)timed out`),
}

// RegisterSelector registers a selector pattern with its associated check function
func registerSelector(selector string, checkFunc CheckFunc) {
	Selectors[selector] = checkFunc
}

func init() {
	// Register all selectors
	registerSelector("Reached PR limit - skipping PR creation", prLimitReached)
	registerSelector("Found renovate config errors", renovateConfigErrors)
	registerSelector("rawExec err", rawExecError)
	registerSelector("Platform-native commit: unknown error", platformCommitError)
}

// extractUsefulError extracts the most useful parts of a potentially long error message.
func extractUsefulError(fullMessage string, maxOutputLines int) string {
	if fullMessage == "" {
		return ""
	}

	lines := strings.Split(fullMessage, "\n")
	// remove trailing empty lines
	if lines[0] == "" {
		lines = lines[1:]
	}
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// If short enough, return as-is
	if len(lines) <= maxOutputLines {
		return strings.TrimSpace(fullMessage)
	}

	return processLongMessage(lines, maxOutputLines)
}

// processLongMessage processes a long message, keeping critical lines and context
func processLongMessage(lines []string, maxOutputLines int) string {
	usefulLines := []string{strings.TrimSpace(lines[0])}
	contextBuffer := make([]string, 0, 2) // keep previous 2 lines of context
	cutLinesCount := 0
	omittedLines := 0

	// Pattern to match lines with only symbols like ~^=
	symbolPattern := regexp.MustCompile(`^\s*[~^=]+\s*$`)

	for i, line := range lines[1:] { // skip first line, already added
		i = i + 1 // adjust index because of slicing
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines or lines with only symbols
		if trimmedLine == "" || symbolPattern.MatchString(trimmedLine) {
			continue
		}

		if i == len(lines)-1 {
			omittedLines = cutLinesCount - len(contextBuffer)
			if omittedLines > 0 {
				usefulLines = append(usefulLines, fmt.Sprintf("[... %d lines omitted ...]", omittedLines))
			}

			usefulLines = append(usefulLines, contextBuffer...)
			usefulLines = append(usefulLines, trimmedLine)
			break
		}

		// Check if we should break and add the last few lines
		if len(usefulLines) >= maxOutputLines {
			omittedLines = cutLinesCount + len(lines) - i - 2 // count the remaining lines except last 3, which we always add
			if omittedLines > 0 {
				usefulLines = append(usefulLines, fmt.Sprintf("[... %d lines omitted ...]", omittedLines))
			}

			// Add the last few lines (very last line is empty after split)
			if i <= len(lines)-3 {
				lastLine4 := strings.TrimSpace(lines[len(lines)-3])
				if lastLine4 != "" && !symbolPattern.MatchString(lastLine4) {
					usefulLines = append(usefulLines, lastLine4)
				}
			}
			if i <= len(lines)-2 {
				lastLine3 := strings.TrimSpace(lines[len(lines)-2])
				if lastLine3 != "" && !symbolPattern.MatchString(lastLine3) {
					usefulLines = append(usefulLines, lastLine3)
				}
			}
			lastLine1 := strings.TrimSpace(lines[len(lines)-1])
			if lastLine1 != "" && !symbolPattern.MatchString(lastLine1) {
				usefulLines = append(usefulLines, lastLine1)
			}
			break
		}

		// Check if this line matches any critical pattern
		if isCriticalLine(trimmedLine) {
			// Add any buffered context lines if we have cut lines
			omittedLines = cutLinesCount - len(contextBuffer)
			if omittedLines > 0 {
				usefulLines = append(usefulLines, fmt.Sprintf("[... %d lines omitted ...]", omittedLines))
			}
			cutLinesCount = 0

			usefulLines = append(usefulLines, contextBuffer...)
			usefulLines = append(usefulLines, trimmedLine)
			contextBuffer = contextBuffer[:0] // clear buffer
		} else {
			cutLinesCount++
			// Add to context buffer (maintaining maxlen=2)
			if len(contextBuffer) >= 2 {
				contextBuffer = contextBuffer[1:] // remove first element
			}
			contextBuffer = append(contextBuffer, trimmedLine)
		}
	}

	return strings.Join(usefulLines, "\n")
}

// isCriticalLine checks if a line matches any critical error pattern
func isCriticalLine(line string) bool {
	for _, pattern := range criticalPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// Default version with maxOutputLines=8
func extractUsefulErrorDefault(fullMessage string) string {
	return extractUsefulError(fullMessage, 8)
}

func prLimitReached(line *LogEntry, report *SimpleReport) {
	report.Warning("PR limit reached - skipping PR creation")
}

// renovateConfigErrors checks for Renovate configuration errors
func renovateConfigErrors(line *LogEntry, report *SimpleReport) {
	var configErrors []string
	errors, ok := line.Extras["errors"].([]interface{})
	if !ok {
		configErrors = append(configErrors, "Unable to parse config errors")
	} else {
		for _, errMap := range errors {
			if errMap, ok := errMap.(map[string]interface{}); ok {
				configErrors = append(configErrors, fmt.Sprintf("\n%s: %s", errMap["topic"], errMap["message"]))
			}
		}
	}
	report.Error("Found renovate config errors", "Errors", strings.Join(configErrors, ""))
}

// rawExecError checks for command execution errors
func rawExecError(line *LogEntry, report *SimpleReport) {
	errData, ok := line.Extras["err"].(map[string]interface{})
	if !ok {
		return
	}

	fields := []interface{}{
		"Branch", line.Extras["branch"],
		"Duration", line.Extras["durationMs"],
	}

	if options, ok := errData["options"].(map[string]interface{}); ok {
		fields = append(fields, "Timeout", options["timeout"])
	}

	message, _ := errData["message"].(string)

	if strings.Contains(message, "Failed to download metadata for repo") {
		fields = append(fields, "Hint", "Possible Red Hat subscription activation key issue")
	}

	fileNotFoundRe := regexp.MustCompile(`FileNotFoundError: \[Errno 2\] No such file or directory: '([\w\/\.\-]+)'`)
	if matches := fileNotFoundRe.FindStringSubmatch(message); matches != nil {
		fields = append(fields, "Hint", fmt.Sprintf("File not found: %s, check rpms.in.yaml configuration", matches[1]))
	}

	fields = append(fields, "Message", extractUsefulErrorDefault(message))

	report.Error("Error executing command", fields...)
}

// platformCommitError checks for platform-native commit errors
func platformCommitError(line *LogEntry, report *SimpleReport) {
	errData, ok := line.Extras["err"].(map[string]interface{})
	if !ok {
		return
	}

	errMessage, _ := errData["message"].(string)
	fullTask := ""
	for _, cmd := range errData["task"].(map[string]interface{})["commands"].([]interface{}) {
		fullTask = fmt.Sprintf("%s %s", fullTask, cmd)
	}

	report.Error(
		line.Msg,
		"Branch", line.Extras["branch"],
		"Message", errMessage,
		"Task", fullTask,
	)
}
