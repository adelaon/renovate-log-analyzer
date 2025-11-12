# Renovate Log Analyzer (used as part of the [MintMaker](https://github.com/konflux-ci/mintmaker) service)
<small>*Original content drafted by Cursor was reviewed and edited*</small>

This repository contains a Go implementation for analyzing Renovate logs and extracting categorized errors, warnings, and info messages. The implementation provides both level-based error extraction and message-based pattern matching for Renovate logs.

Another part of this repo is the KITE client, which takes the information after analyzing the logs and sends it to [KITE API](https://github.com/konflux-ci/kite) to be displayed on an Issue dashboard in Konflux UI.

This service is meant to run as last step of a `tekton pipeline` created by the [MintMaker controller](https://github.com/konflux-ci/mintmaker).

## Log analyzer

- **`checks.go`**: Check definitions with selector registration for message-based pattern matching
- **`models.go`**: Data models including `LogEntry`, `PodDetails`, and `SimpleReport`
- **`report.go`**: Simple report functionality for collecting categorized messages
- **`log_reader.go`**: Log processing logic for extracting logs from a `json` file and parsing them into `Go` object

## Architecture

### Dual Processing Approach

The implementation provides two complementary approaches for log analysis:

1. **Level-based extraction**: Extracts ERROR and FATAL messages based on log level for `FailureLogs` (used only in the case of a failed PipelineRun)
2. **Message-based extraction**: Uses pattern matching to categorize messages into errors, warnings, and info (used always)

### Selector Pattern - main logic taken from [mintmaker-e2e logdoc checks](https://gitlab.cee.redhat.com/rsaar/mintmaker-e2e/-/tree/main/tools?ref_type=heads)

The message-based approach uses selector pattern matching:

```go
// Register a selector at initialization
func init() {
    RegisterSelector("Base branch does not exist - skipping", baseBranchDoesNotExist)
}

// Check function
func baseBranchDoesNotExist(line *LogEntry, report *SimpleReport) {
    report.Error("Base branch does not exist", 
        "hint", "Check `baseBranchPatterns` in renovate.json")
}
```

### Simple Report System

The implementation uses a simple report system:

```go
type SimpleReport struct {
    Errors   []string
    Warnings []string
    Infos    []string
}

func (r *SimpleReport) Error(msg string, fields ...interface{}) {
    // Format and add to Errors slice
}
```

## Usage Example

```go
// Process logs from a failed pod
func GetFailedPodDetails(ctx context.Context, client client.Client, Clientset *kubernetes.Clientset, pipelineRun *tektonv1.PipelineRun) (*PodDetails, error) {
    // The function automatically processes logs and returns structured results
    return &PodDetails{
        Name:        taskRun.Status.PodName,
        Namespace:   pipelineRun.Namespace,
        TaskName:    getTaskRunTaskName(taskRun),
        FailureLogs: reason,          // Level-based errors (ERROR/FATAL)
        Error:       report.Errors,   // Message-based errors
        Warning:     report.Warnings, // Message-based warnings
        Info:        report.Infos,    // Message-based info
    }, nil
}
```

## Selector List

All selectors from the [mintmaker-e2e logdoc checks](https://gitlab.cee.redhat.com/rsaar/mintmaker-e2e/-/tree/main/tools?ref_type=heads) are implemented with some changes:

1. `"Reached PR limit - skipping PR creation"` - Warning
2. `"Base branch does not exist - skipping"` - Error
3. `"Config migration necessary"` - Warning
4. `"Config needs migration"` - Warning
5. `"Found renovate config errors"` - Error
6. `"branches info extended"` - Info
7. `"PR rebase requested=true"` - Info
8. `"rawExec err"` - Error
9. `"Ignoring upgrade collision"` - Warning
10. `"Platform-native commit: unknown error"` - Error
11. `"File contents are invalid JSONC but parse using JSON5"` - Error
12. `"Repository has changed during renovation - aborting"` - Error
13. `"Passing repository-changed error up"` - Error

## Log Levels

Following [Renovate documentation](https://docs.renovatebot.com/troubleshooting/):

- **TRACE**: 10
- **DEBUG**: 20
- **INFO**: 30
- **WARN**: 40
- **ERROR**: 50
- **FATAL**: 60

## ExtractUsefulError Function

The `ExtractUsefulError` function intelligently extracts the most useful parts of potentially long error messages. It's designed to reduce noise while preserving critical information and context.

### How It Works

1. **Preserves the first line**: Always keeps the initial error message for context
2. **Identifies critical lines**: Uses regex patterns to detect important error lines (e.g., "Command failed:", "Error:", "FATAL:", "Caused by:", etc.)
3. **Maintains context**: Keeps a rolling buffer of recent non-critical lines for context
4. **Preserves the end**: Always includes the last few lines of the error message
5. **Filters noise**: Skips empty lines and lines containing only symbols (like `~`, `^`, `=`)
6. **Limits output**: Restricts output to a maximum number of lines (default: 8) to keep messages concise (it can be a little bit more, because of the last 3 lines being added after the max length check)

### Example

The function transforms verbose error messages into concise, actionable summaries. The images below demonstrate the transformation:

**Before** - Full verbose error message with many lines of stack traces and context:

![Before: Full error message](before.png)

**After** - Same error after processing with `ExtractUsefulError`, highlighting only the critical parts:

![After: Extracted useful error](after.png)

The function is used automatically in the `rawExecError` check function to provide cleaner, more readable error messages in reports.

## KITE client
- **`client.go`**: Contains everything needed to communicate with the [KITE API backend](https://github.com/konflux-ci/kite/tree/main/packages/backend) - defines Payload structures, initializes the client and contains functions to send requests

## Local Testing

To test the log analyzer locally using `go run ./cmd/log-analyzer/main.go` the following set up is needed:

### Required Environment Variables

The application requires the following environment variables:

- **`POD_NAME`**: Name of a pod in your Kubernetes cluster (must exist and have container statuses)
- **`NAMESPACE`**: Kubernetes namespace where the pod exists
- **`KITE_API_URL`**: URL to the KITE API endpoint
- **`LOG_FILE`**: Path to the Renovate log file (there is a `test-logs.json` file in the root folder for testing purposes)

### Kubernetes Configuration

The application automatically detects Kubernetes configuration:

1. **In-cluster config**: When running inside a Kubernetes cluster, it uses the in-cluster configuration
2. **Kubeconfig fallback**: For local development, it falls back to:
   - `KUBECONFIG` environment variable (if set)
   - `~/.kube/config` (default location)

Ensure you have a valid kubeconfig file with access to the cluster where your test pod exists.

### Test Log File Format

The log file should contain Renovate JSON logs, with each line being a separate JSON object. Example:

```json
{"level": 50, "msg": "rawExec err", "err": {"message": "Command failed: npm install"}, "branch": "main"}
{"level": 40, "msg": "Reached PR limit - skipping PR creation"}
{"level": 30, "msg": "branches info extended", "branchesInformation": [...]}
{"level": 50, "msg": "Base branch does not exist - skipping", "baseBranch": "feature/old"}
```

### Example Test Command

```bash
cd cmd/log-analyzer

# Set required environment variables
export POD_NAME=test-pod-name
export NAMESPACE=random
export KITE_API_URL=placeholder     # or actual KITE API URL
export LOG_FILE="./test-logs.json"  # path to test log file

# Run the application
go run ./cmd/log-analyzer/main.go
```

### Notes

- **Pod, Namespace and Kite API URL do not have to exist**: For testing the log parsing function, those values can be placeholders. The service will not be able to send the webhooks, or determin the pod status, but it will parse the json logs from file and display result via logs (in tha same terminal where `go run ./cmd/log-analyzer/main.go` is run).
- **Log file location**: It is necessary to ensure the log file path is correct and the file is readable