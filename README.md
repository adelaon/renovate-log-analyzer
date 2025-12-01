# Renovate Log Analyzer

A Go-based tool for analyzing [Renovate](https://github.com/renovatebot/renovate) logs and reporting issues to [Kite API](https://github.com/konflux-ci/kite). Part of the [MintMaker](https://github.com/konflux-ci/mintmaker) ecosystem.

## Overview

This tool runs as the final step of Tekton pipelines created by the [MintMaker controller](https://github.com/konflux-ci/mintmaker). It analyzes Renovate JSON logs, extracts and categorizes issues, and reports them to Kite API for display in the Konflux UI Issues dashboard.

### Key Features

- **Dual Processing**: Level-based (ERROR/FATAL) and message-based pattern matching
- **Smart Error Extraction**: Condenses verbose stack traces to essential information
- **Categorization**: Automatically categorizes issues as Errors, Warnings, or Infos
- **Kite Integration**: Sends webhook notifications to Kite API with analyzed results

## How It Works

1. **Log Processing**: Reads Renovate JSON logs and extracts ERROR (level 50) and FATAL (level 60) entries
2. **Error Aggregation**: Aggregates level-based errors by message with duplicate tracking
3. **Selector Checks**: Pattern matches log messages against predefined selectors to extract meaningful issues
4. **Health Check**: Verifies Kite API availability before sending webhooks
5. **Webhook Notification**: Sends `pipeline-success`, `pipeline-failure`, or `mintmaker-custom` webhooks based on findings

## Quick Start

```bash
# Set required environment variables
export NAMESPACE=your-namespace
export KITE_API_URL=https://kite-api.example.com
export GIT_HOST=github.com
export REPOSITORY=owner/repo
export BRANCH=main
export LOG_FILE="./pkg/doctor/testdata/test_logs.json"

# Run the analyzer
go run ./cmd/log-analyzer/main.go --dev
```

## Documentation

For detailed documentation, see [docs/README.md](docs/README.md), which includes:

- Architecture and implementation details
- Complete selector list with examples
- `extractUsefulError` function documentation
- Local testing guide
- Kite client documentation

## Environment Variables

### Required
- **`NAMESPACE`**: Kubernetes namespace
- **`KITE_API_URL`**: URL to the Kite API endpoint

### Optional
- **`GIT_HOST`**: Git host (default: "unknown")
- **`REPOSITORY`**: Repository name (default: "unknown")
- **`BRANCH`**: Branch name (default: "unknown")
- **`LOG_FILE`**: Path to log file (default: `/workspace/shared-data/renovate-logs.json`)
- **`PIPELINE_RUN`**: Pipeline run identifier (default: "unknown")

### Flags
- **`--dev`**: Enable development mode with debug logging and source locations

## Project Structure

```
renovate-log-analyzer/
├── cmd/
│   └── log-analyzer/
│       └── main.go          # Entry point
├── pkg/
│   ├── doctor/              # Log analysis package
│   │   ├── checks.go        # Selector definitions
│   │   ├── models.go        # Data models
│   │   ├── report.go        # Report generation
│   │   └── log_reader.go    # Log processing
│   └── kite/                # Kite API client
│       └── client.go
└── docs/
    └── README.md            # Detailed documentation
```

## License

Licensed under the Apache License, Version 2.0. See [licenses/LICENSE](licenses/LICENSE) for details.