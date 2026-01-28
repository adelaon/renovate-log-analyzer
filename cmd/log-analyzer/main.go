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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/konflux-ci/renovate-log-analyzer/pkg/doctor"
	"github.com/konflux-ci/renovate-log-analyzer/pkg/kite"
)

func main() {
	if err := run(); err != nil {
		handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{})
		logger := slog.New(handler)
		logger.Error("application failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	const defaultLogFilePath = "/workspace/shared-data/renovate-logs.json"

	// Set up slog logger
	devMode := flag.Bool("dev", false, "Enable development mode (more verbose)")
	flag.Parse()

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}
	if *devMode {
		opts.Level = slog.LevelDebug
		opts.AddSource = true // Show source location in dev mode
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler).With("name", "log-analyzer")

	// Get the necessary environment variables
	kiteAPIURL := getEnvOrDefault("KITE_API_URL", "")
	authTokenPath := getEnvOrDefault("KITE_AUTH_TOKEN_FILE", "")
	namespace := getEnvOrDefault("NAMESPACE", "")

	logFilePath := getEnvOrDefault("LOG_FILE", defaultLogFilePath)

	pipelineRunName := getEnvOrDefault("PIPELINE_RUN", "unknown")
	gitHost := getEnvOrDefault("GIT_HOST", "unknown")
	repository := getEnvOrDefault("REPOSITORY", "unknown")
	branch := getEnvOrDefault("BRANCH", "unknown")
	logger = logger.With(
		"pipelineRun", pipelineRunName,
		"gitHost", gitHost,
		"repository", repository,
		"branch", branch,
	)

	if namespace == "" || kiteAPIURL == "" || authTokenPath == "" {
		return fmt.Errorf("missing required environment variables: NAMESPACE, KITE_API_URL and KITE_AUTH_TOKEN_FILE must be set")
	}
	logger = logger.With("namespace", namespace)

	tokenBytes, err := os.ReadFile(authTokenPath)
	if err != nil {
		return fmt.Errorf("failed to read token from %s: %w", authTokenPath, err)
	}
	authToken := strings.TrimSpace(string(tokenBytes))

	// Now use the logger throughout your code
	logger.Info("Starting log analyzer tool")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pipelineIdentifier := fmt.Sprintf("%s/%s@%s", gitHost, repository, branch)

	// Step 2: Process logs if step-renovate ran
	var processedFailReason string
	processedFailReason, report, err := doctor.ProcessLogFile(ctx, logFilePath)
	if err != nil {
		// Exit since we couldn't analyze logs at all
		return fmt.Errorf("failed to process logs: %w", err)
	}
	logger.Info("Successfully processed logs",
		"failureLogs", processedFailReason,
		"reportErrors", report.Errors,
		"reportWarnings", report.Warnings,
		"reportInfos", report.Infos,
	)

	if *devMode {
		fmt.Println("----- Log Analysis Result -----")
		fmt.Println("Fail logs:\n", processedFailReason)
		fmt.Println("Report Errors:\n", strings.Join(report.Errors, "\n-------------\n"))
		fmt.Println("Report Warnings:\n", strings.Join(report.Warnings, "\n-------------\n"))
		fmt.Println("-----------------------------")
	}

	// Create Kite client
	kiteClient, err := kite.NewClient(kiteAPIURL, authToken)
	if err != nil {
		return fmt.Errorf("failed to create Kite client for %s: %w", kiteAPIURL, err)
	}

	kiteStatus, err := kiteClient.GetKiteStatus(ctx)
	if err != nil {
		return fmt.Errorf("request for Kite API status failed at %s: %w", kiteAPIURL, err)
	}
	logger.Info("Kite API status request completed", "status", kiteStatus, "apiURL", kiteAPIURL)

	// Send custom webhooks (only if we have log analysis)
	if len(report.Errors) > 0 || len(report.Warnings) > 0 || len(report.Infos) > 0 {
		sendCustomWebhooks(ctx, logger, kiteClient, namespace, pipelineIdentifier, report)
	}

	// Send success or failure webhook
	if processedFailReason == "" {
		if err := sendSuccessWebhook(ctx, kiteClient, namespace, pipelineIdentifier); err != nil {
			return fmt.Errorf("failed to send success webhook: %w", err)
		}
		logger.Info("Successfully sent success webhook")
	} else {
		if err := sendFailureWebhook(ctx, kiteClient, namespace, pipelineIdentifier,
			pipelineRunName, processedFailReason); err != nil {
			return fmt.Errorf("failed to send failure webhook: %w", err)
		}
		logger.Info("Successfully sent failure webhook", "failureMsg", processedFailReason)
	}

	logger.Info("Successfully completed log analysis and sent webhook")
	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

func sendCustomWebhooks(ctx context.Context, logger *slog.Logger, kiteClient *kite.Client, namespace, pipelineIdentifier string, report *doctor.SimpleReport) {
	sentTypes := ""
	if len(report.Errors) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "error", report.Errors); err != nil {
			logger.Error("failed to send error webhook", "err", err)
		} else {
			sentTypes += "error "
		}
	}
	if len(report.Warnings) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "warning", report.Warnings); err != nil {
			logger.Error("failed to send warning webhook", "err", err)
		} else {
			sentTypes += "warning "
		}
	}
	if len(report.Infos) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "info", report.Infos); err != nil {
			logger.Error("failed to send info webhook", "err", err)
		} else {
			sentTypes += "info"
		}
	}
	if sentTypes != "" {
		logger.Info("Successfully sent custom webhooks", "types", sentTypes)
	} else {
		logger.Info("Custom webhooks were not sent", "errors", len(report.Errors), "warnings", len(report.Warnings), "infos", len(report.Infos))
	}
}

func sendCustomWebhook(ctx context.Context, kiteClient *kite.Client, namespace, pipelineIdentifier, issueType string, logs []string) error {
	payload := kite.CustomPayload{
		PipelineId: pipelineIdentifier,
		Namespace:  namespace,
		Type:       issueType,
		Logs:       logs,
	}

	marshaledPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("unable to marshal payload: %w", err)
	}

	return kiteClient.SendWebhookRequest(ctx, namespace, "mintmaker-custom", marshaledPayload)
}

func sendSuccessWebhook(ctx context.Context, kiteClient *kite.Client, namespace, pipelineIdentifier string) error {
	payload := kite.PipelineSuccessPayload{
		PipelineName: pipelineIdentifier,
		Namespace:    namespace,
	}

	marshaledPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("unable to marshal payload: %w", err)
	}

	return kiteClient.SendWebhookRequest(ctx, namespace, "pipeline-success", marshaledPayload)
}

func sendFailureWebhook(ctx context.Context, kiteClient *kite.Client, namespace, pipelineIdentifier, runID, failReason string) error {
	payload := kite.PipelineFailurePayload{
		PipelineName:  pipelineIdentifier,
		Namespace:     namespace,
		FailureReason: failReason,
		RunID:         runID,
		LogsURL:       "",
	}

	marshaledPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("unable to marshal payload: %w", err)
	}

	return kiteClient.SendWebhookRequest(ctx, namespace, "pipeline-failure", marshaledPayload)
}
