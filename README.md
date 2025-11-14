# Renovate Log Analyzer (used as part of the [MintMaker](https://github.com/konflux-ci/mintmaker) service)
<small>*Original content drafted by Cursor was reviewed and edited*</small>

This repository contains a Go implementation for analyzing Renovate logs. The implementation provides level-based error (and fatal) extraction.

Another part of this repo is the KITE client, which in the event of a failure in Renovate sends the extracted errors to the [KITE API](https://github.com/konflux-ci/kite) to be displayed on an Issue dashboard in Konflux UI.

This service is meant to run as last step of `tekton pipeline` created by the [MintMaker controller](https://github.com/konflux-ci/mintmaker).

## Log analyzer

- **`models.go`**: Data models (`LogEntry` and `PodDetails`)
- **`log_reader.go`**: Log processing logic for extracting logs from a `json` file and parsing them into `Go` object as well as the analyzing logic

## Log Levels

Following [Renovate documentation](https://docs.renovatebot.com/troubleshooting/):

- **TRACE**: 10
- **DEBUG**: 20
- **INFO**: 30
- **WARN**: 40
- **ERROR**: 50
- **FATAL**: 60

The log-analyzer looks for errors with level 50 and 60 and extracts the most useful information from them by looking through all the opssible fields in their structure.

## KITE client
- **`client.go`**: Contains everything needed to communicate with the [KITE API backend](https://github.com/konflux-ci/kite/tree/main/packages/backend) - defines Payload structures, initializes the client and contains functions to send requests

## Local Testing

To test the log analyzer locally using `go run ./cmd/log-analyzer/main.go` the following set up is needed:

### Required Environment Variables

The application requires the following environment variables:

- **`POD_NAME`**: Name of a pod in your Kubernetes cluster (must exist)
- **`NAMESPACE`**: Kubernetes namespace where the pod exists
- **`KITE_API_URL`**: URL to the KITE API endpoint
- **`LOG_FILE`**: Path to the Renovate log file (there is a `fatal_exit_logs.json` file in the root folder for testing purposes)

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
# Set required environment variables
export POD_NAME=pod-name
export NAMESPACE=namespace-name
export KITE_API_URL=placeholder             # or actual KITE API URL
export LOG_FILE="./fatal_exit_logs.json"    # path to test log file

# Run the application
go run ./cmd/log-analyzer/main.go
```

### Notes

- **Pod and Namespace**: Has to be a valid pod name and namespace name, where this pod exists (for testing the log analyzing, it can be any pod - does not have to be from the mintmaker pipeline).
- **Kite API URL**: Does not have to be working endpoint for testing the log parsing function. The service will not be able to send the webhooks, but it will parse the json logs from file and display result via logs (in tha same terminal where `go run ./cmd/log-analyzer/main.go` is run).
- **Log file location**: It is necessary to ensure the log file path is correct and the file is readable