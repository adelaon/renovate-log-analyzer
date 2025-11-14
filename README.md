# Renovate Log Analyzer (used as part of the [MintMaker](https://github.com/konflux-ci/mintmaker) service)

This repository contains a Go implementation for analyzing Renovate logs. The implementation provides level-based error (and fatal) extraction.

Another part of this repo is the Kite client, which in the event of a failure in Renovate sends the extracted errors to the [Kite API](https://github.com/konflux-ci/kite) to be displayed on an Issue dashboard in Konflux UI.

This service is meant to run as last step of `tekton pipeline` created by the [MintMaker controller](https://github.com/konflux-ci/mintmaker).

## Log analyzer

- **`models.go`**: Data models (`LogEntry`)
- **`log_reader.go`**: Log processing logic for extracting logs from a `json` file and parsing them into `Go` object as well as the analyzing logic

## Log Levels

Following [Renovate documentation](https://docs.renovatebot.com/troubleshooting/):

- **TRACE**: 10
- **DEBUG**: 20
- **INFO**: 30
- **WARN**: 40
- **ERROR**: 50
- **FATAL**: 60

The log-analyzer looks for errors with level 50 (ERROR) and 60 (FATAL) and extracts the most useful information from them by looking through all the possible fields in their structure. It aggregates duplicate errors and formats them into a summary message.

## Kite client
- **`client.go`**: Contains everything needed to communicate with the [Kite API backend](https://github.com/konflux-ci/kite/tree/main/packages/backend) - defines Payload structures, initializes the client and contains functions to send requests. The client checks Kite API health status and sends webhooks for pipeline success or failure.

## Local Testing

### Command Line Flags

- **`-dev`**: Enable development mode with more verbose logging and source location (default: false)

To test the log analyzer locally using `go run ./cmd/log-analyzer/main.go` the following set up is needed:

### Required Environment Variables

The application requires the following environment variables:

- **`NAMESPACE`**: Kubernetes namespace (required)
- **`KITE_API_URL`**: URL to the Kite API endpoint (required)
- **`GIT_HOST`**: Git host (e.g., github.com) (optional)
- **`REPOSITORY`**: Repository name (optional)
- **`BRANCH`**: Branch name (optional)
- **`LOG_FILE`**: Path to the Renovate log file (optional, defaults to `/workspace/shared-data/renovate-logs.json`)
- **`PIPELINE_RUN`**: Pipeline run identifier (optional, defaults to "unknown")

### Test Log File Format

The log file should contain Renovate JSON logs, with each line being a separate JSON object. Example:

```json
{"level": 20, "msg": "rawExec err", "err": {"message": "Command failed: npm install"}, "branch": "main"}
{"level": 40, "msg": "Reached PR limit - skipping PR creation"}
{"level": 30, "msg": "branches info extended", "branchesInformation": [...]}
{"level": 50, "msg": "Base branch does not exist - skipping", "baseBranch": "feature/old"}
{"level": 60, "msg": "Fatal error occurred", "err": {"message": "Critical failure"}}
```

### Example Test Command

```bash
# Set required environment variables
export NAMESPACE=namespace-name
export KITE_API_URL=https://kite-api.example.com            # or placeholder for testing
export GIT_HOST=github.com
export REPOSITORY=owner/repo
export BRANCH=main
export LOG_FILE="./pkg/doctor/testdata/fatal_exit_logs.json" # path to test log file
export PIPELINE_RUN=test-run-123                            # optional

# Run the application
go run ./cmd/log-analyzer/main.go
```

### How It Works

1. **Log Processing**: The application reads the log file and extracts ERROR (level 50) and FATAL (level 60) entries.

2. **Error Aggregation**: Errors are aggregated by message, with duplicate counts tracked.

3. **Kite API Health Check**: Before sending webhooks, the application checks the Kite API health status.

4. **Webhook Notification**:
   - If no errors are found, sends a `pipeline-success` webhook
   - If errors are found, sends a `pipeline-failure` webhook with the aggregated failure reason

5. **Pipeline Identifier**: The pipeline identifier is constructed as `{GIT_HOST}/{REPOSITORY}@{BRANCH}`.

### Notes

- **Kite API URL**: For testing log parsing only, the Kite API URL does not need to be a working endpoint. The service will parse the JSON logs from the file and display results via logs, but webhook sending will fail if the API is not accessible.
- **Log file location**: Ensure the log file path is correct and the file is readable. If `LOG_FILE` is not set, it defaults to `/workspace/shared-data/renovate-logs.json`.
- **Error handling**: The application exits with code 1 if any step fails (missing environment variables, log processing errors, API failures, etc.)