# Renovate Log Analyzer - Detailed Documentation

This document provides comprehensive documentation for the Renovate Log Analyzer tool, which is part of the [MintMaker](https://github.com/konflux-ci/mintmaker) ecosystem.

## Table of Contents

- [Log Analyzer Components](#log-analyzer-components)
- [Architecture](#architecture)
  - [Dual Processing Approach](#dual-processing-approach)
  - [Selector Pattern](#selector-pattern)
  - [Simple Report System](#simple-report-system)
- [Selector List](#selector-list)
- [Log Levels](#log-levels)
- [extractUsefulError Function](#extractusefulerror-function)
  - [How It Works](#how-it-works)
  - [Example](#example)
- [Kite Client](#kite-client)
- [Local Testing](#local-testing)
  - [Command Line Flags](#command-line-flags)
  - [Required Environment Variables](#required-environment-variables)
  - [Test Log File Format](#test-log-file-format)
  - [Example Test Command](#example-test-command)
  - [How It Works](#how-it-works-1)
  - [Notes](#notes)

## Overview

This tool analyzes Renovate logs and sends categorized results to [Kite API](https://github.com/konflux-ci/kite) for display in the Konflux UI Issues dashboard. It runs as the last step of `tekton pipeline` created by the [MintMaker controller](https://github.com/konflux-ci/mintmaker).

## Log Analyzer Components

- **`checks.go`**: Check definitions with selector registration for message-based pattern matching
- **`models.go`**: Data models (`LogEntry` and `SimpleReport`)
- **`report.go`**: Simple report functionality for collecting categorized messages
- **`log_reader.go`**: Log processing logic for extracting logs from a `json` file and parsing them into `Go` object

## Architecture

### Dual Processing Approach

The implementation provides two complementary approaches for log analysis:

1. **Level-based extraction**: Extracts ERROR and FATAL messages based on log level for `processedFailReason`
2. **Message-based extraction**: Uses pattern matching to categorize messages into errors, warnings, and info

### Selector Pattern

The message-based approach uses selector pattern matching:

```go
// Register a selector at initialization
func init() {
    registerSelector("Reached PR limit - skipping PR creation", prLimitReached)
}

// Check function
func prLimitReached(line *LogEntry, report *SimpleReport) {
	report.Warning("PR limit reached - skipping PR creation")
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

## Selector List

1. `"Reached PR limit - skipping PR creation"` - Warning
2. `"Found renovate config errors"` - Error
3. `"rawExec err"` - Error
4. `"Platform-native commit: unknown error"` - Error

## Log Levels

Following [Renovate documentation](https://docs.renovatebot.com/troubleshooting/):

- **TRACE**: 10
- **DEBUG**: 20
- **INFO**: 30
- **WARN**: 40
- **ERROR**: 50
- **FATAL**: 60

## extractUsefulError Function

The `extractUsefulError` function intelligently extracts the most useful parts of potentially long error messages. It's designed to reduce noise while preserving critical information and context.

### How It Works

1. **Preserves the first line**: Always keeps the initial error message for context
2. **Identifies critical lines**: Uses regex patterns to detect important error lines (e.g., "Command failed:", "Error:", "FATAL:", "Caused by:", etc.)
3. **Maintains context**: Keeps a rolling buffer of recent non-critical lines for context
4. **Preserves the end**: Always includes the last few lines of the error message
5. **Filters noise**: Skips empty lines and lines containing only symbols (like `~`, `^`, `=`)
6. **Limits output**: Restricts output to a maximum number of lines (default: 8) to keep messages concise (it can be a little bit more, because of the last 3 lines being added after the max length check)

### Example

The function transforms verbose error messages into concise, actionable summaries. Below is an example of the transformation:

**Before** - Full verbose error message with many lines of stack traces and context:
```console
Command failed: hashin h11==0.16.0 -r python/kserve/requirements.txt
Traceback (most recent call last):
  File "/usr/lib64/python3.12/urllib/request.py", line 1344, in do_open
    h.request(req.get_method(), req.selector, req.data, headers,
  File "/usr/lib64/python3.12/http/client.py", line 1338, in request
    self._send_request(method, url, body, headers, encode_chunked)
  File "/usr/lib64/python3.12/http/client.py", line 1384, in _send_request
    self.endheaders(body, encode_chunked=encode_chunked)
  File "/usr/lib64/python3.12/http/client.py", line 1333, in endheaders
    self._send_output(message_body, encode_chunked=encode_chunked)
  File "/usr/lib64/python3.12/http/client.py", line 1093, in _send_output
    self.send(msg)
  File "/usr/lib64/python3.12/http/client.py", line 1037, in send
    self.connect()
  File "/usr/lib64/python3.12/http/client.py", line 1479, in connect
    self.sock = self._context.wrap_socket(self.sock,
                ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/ssl.py", line 455, in wrap_socket
    return self.sslsocket_class._create(
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/ssl.py", line 1041, in _create
    self.do_handshake()
  File "/usr/lib64/python3.12/ssl.py", line 1319, in do_handshake
    self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: unable to get local issuer certificate (_ssl.c:1010)

During handling of the above exception, another exception occurred:

Traceback (most recent call last):
  File "/home/renovate/.local/bin/hashin", line 7, in <module>
    sys.exit(main())
             ^^^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 832, in main
    return run(
           ^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 120, in run
    return run_packages(specs, requirements_file, *args, **kwargs)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 175, in run_packages
    data = get_package_hashes(
           ^^^^^^^^^^^^^^^^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 675, in get_package_hashes
    data = get_package_data(package, index_url, verbose)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 603, in get_package_data
    content = json.loads(_download(url))
                         ^^^^^^^^^^^^^^
  File "/home/renovate/.local/share/pipx/venvs/hashin/lib64/python3.12/site-packages/hashin.py", line 81, in _download
    r = urlopen(url)
        ^^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 215, in urlopen
    return opener.open(url, data, timeout)
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 515, in open
    response = self._open(req, data)
               ^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 532, in _open
    result = self._call_chain(self.handle_open, protocol, protocol +
             ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 492, in _call_chain
    result = func(*args)
             ^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 1392, in https_open
    return self.do_open(http.client.HTTPSConnection, req,
           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
  File "/usr/lib64/python3.12/urllib/request.py", line 1347, in do_open
    raise URLError(err)
urllib.error.URLError: <urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: unable to get local issuer certificate (_ssl.c:1010)>
```

**After** - Same error after processing with `extractUsefulError`, highlighting only the critical parts:
```console
Command failed: hashin h11==0.16.0 -r python/kserve/requirements.txt
File "/home/renovate/.local/bin/hashin", line 7, in <module>
sys.exit(main())
[... 17 lines omitted ...]
File "/usr/lib64/python3.12/ssl.py", line 1319, in do_handshake
self._sslobj.do_handshake()
ssl.SSLCertVerificationError: [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: unable to get local issuer certificate (_ssl.c:1010)
During handling of the above exception, another exception occurred:
[... 25 lines omitted ...]
File "/usr/lib64/python3.12/urllib/request.py", line 1347, in do_open
raise URLError(err)
urllib.error.URLError: <urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: unable to get local issuer certificate (_ssl.c:1010)>
```

The function is used automatically in the `rawExecError` check function to provide cleaner, more readable error messages in reports.

## Kite Client

The Kite client (`client.go`) handles all communication with the [Kite API backend](https://github.com/konflux-ci/kite/tree/main/packages/backend):

- **Payload Structures**: Defines `PipelineFailurePayload`, `PipelineSuccessPayload`, and `CustomPayload`
- **Client Initialization**: Creates HTTP client with 30-second timeout
- **Health Checks**: Verifies Kite API availability via `/api/v1/health` endpoint
- **Webhook Sending**: Posts to `/api/v1/webhooks/{webhook-name}` with namespace in query parameters

### Webhook Types

1. **`pipeline-success`**: Sent when no level-based errors are found
2. **`pipeline-failure`**: Sent when ERROR or FATAL level entries exist
3. **`mintmaker-custom`**: Sent for categorized issues (errors, warnings, infos) discovered by selectors

## Local Testing

### Command Line Flags

- **`-dev`**: Enable development mode with more verbose logging, source location and the results printed into the console (default: false)

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
export NAMESPACE=namespace-name                             # placeholder for testing
export KITE_API_URL=https://kite-api.example.com            # placeholder for testing
export GIT_HOST=github.com                                  # optional
export REPOSITORY=owner/repo                                # optional
export BRANCH=main                                          # optional
export LOG_FILE="./pkg/doctor/testdata/test_logs.json" # path to test log file (or /test_logs.json for testing the string-based categorization)
export PIPELINE_RUN=test-run-123                            # optional

# Run the application
go run ./cmd/log-analyzer/main.go --dev
```

### How It Works

1. **Log Processing**: The application reads the log file and extracts ERROR (level 50) and FATAL (level 60) entries. 

2. **Error Aggregation**: Level based errors are aggregated by message, with duplicate counts tracked.

3. **Check against selectors**: Checks against the integrated Selectors are performed for each parsed log entry. Only the interesting log messages (with additional information extracted from logs) are kept in categorised groups (Errors, Warnings, Infos).

3. **Kite API Health Check**: Before sending webhooks, the application checks the Kite API health status.

4. **Webhook Notification**:
   - If no errors are found, sends a `pipeline-success` webhook
   - If errors are found, sends a `pipeline-failure` webhook with the aggregated failure reason
   - If errors, warnings or infos are present in the generated report, send the corresponding custom `mintmaker-custom` webhook request.

5. **Pipeline Identifier**: The pipeline identifier is constructed as `{GIT_HOST}/{REPOSITORY}@{BRANCH}`.

### Notes

- **Kite API URL**: For testing log parsing only, the Kite API URL does not need to be a working endpoint. The tool will parse the JSON logs from the file and display results via logs, but webhook sending will fail if the API is not accessible.
- **Log file location**: Ensure the log file path is correct and the file is readable. If `LOG_FILE` is not set, it defaults to `/workspace/shared-data/renovate-logs.json`.
- **Error handling**: The application exits with code 1 if any step fails (missing environment variables, log processing errors, API failures, etc.)