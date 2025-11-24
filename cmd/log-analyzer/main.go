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
	kiteAPIURL := os.Getenv("KITE_API_URL")
	namespace := os.Getenv("NAMESPACE")

	logFilePath := os.Getenv("LOG_FILE")
	if logFilePath == "" {
		logFilePath = defaultLogFilePath
	}
	pipelineRunName := os.Getenv("PIPELINE_RUN")
	if pipelineRunName == "" {
		pipelineRunName = "unknown"
	}
	gitHost := os.Getenv("GIT_HOST")
	if gitHost == "" {
		gitHost = "unknown"
	}
	repository := os.Getenv("REPOSITORY")
	if repository == "" {
		repository = "unknown"
	}
	branch := os.Getenv("BRANCH")
	if branch == "" {
		branch = "unknown"
	}
	logger = logger.With(
		"pipelineRun", pipelineRunName,
		"gitHost", gitHost,
		"repository", repository,
		"branch", branch,
	)

	if namespace == "" || kiteAPIURL == "" {
		return fmt.Errorf("missing required environment variables: NAMESPACE and KITE_API_URL must be set")
	}
	logger = logger.With("namespace", namespace)

	// Now use the logger throughout your code
	logger.Info("Starting log analyzer service")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pipelineIdentifier := fmt.Sprintf("%s/%s@%s", gitHost, repository, branch)

	// Step 2: Process logs if step-renovate ran
	var processedFailReason string
	processedFailReason, err := doctor.ProcessLogFile(ctx, logFilePath)
	if err != nil {
		// Exit since we couldn't analyze logs at all
		return fmt.Errorf("failed to process logs: %w", err)
	}
	logger.Info("Successfully processed logs", "failureLogs", processedFailReason)

	// Create Kite client
	kiteClient, err := kite.NewClient(kiteAPIURL)
	if err != nil {
		return fmt.Errorf("failed to create Kite client for %s: %w", kiteAPIURL, err)
	}

	kiteStatus, err := kiteClient.GetKiteStatus(ctx)
	if err != nil {
		return fmt.Errorf("request for Kite API status failed at %s: %w", kiteAPIURL, err)
	}
	logger.Info("Kite API status request completed", "status", kiteStatus, "apiURL", kiteAPIURL)

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
