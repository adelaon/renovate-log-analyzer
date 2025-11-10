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
	"os"
	"os/signal"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appstudiov1alpha1 "github.com/konflux-ci/application-api/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	"github.com/konflux-ci/renovate-log-analyzer/internal/pkg/kite"
	doctor "github.com/konflux-ci/renovate-log-analyzer/internal/pkg/log-analyzer"
)

var (
	log = ctrl.Log.WithName("log-analyzer")
)

func main() {
	// Set up zap logger exactly like manager
	opts := zap.Options{
		Development: true, // Set to true for development (more verbose)
	}

	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	// Set the logger (same as manager)
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Parse your custom flags first
	podName := os.Getenv("POD_NAME")
	namespace := os.Getenv("NAMESPACE")
	kiteAPIURL := os.Getenv("KITE_API_URL")
	logFilePath := os.Getenv("LOG_FILE")

	if logFilePath == "" {
		logFilePath = "/workspace/shared-data/renovate-logs.json"
	}

	if podName == "" || namespace == "" || kiteAPIURL == "" {
		log.Error(fmt.Errorf("missing required environment variables"), "POD_NAME, NAMESPACE, and KITE_API_URL must be set")
		os.Exit(1)
	}

	// Now use the logger throughout your code
	log.Info("Starting log analyzer service",
		"podName", podName,
		"namespace", namespace,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize clients
	cfg, _ := rest.InClusterConfig()
	clientset, _ := kubernetes.NewForConfig(cfg)
	k8sClient, _ := createK8sClient(cfg)

	// Get PipelineRun info
	pod, _ := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	pipelineRunName := pod.Labels["tekton.dev/pipelineRun"]

	pipelineRun := &tektonv1.PipelineRun{}
	k8sClient.Get(ctx, types.NamespacedName{
		Name:      pipelineRunName,
		Namespace: namespace,
	}, pipelineRun)

	// Extract component info
	componentName := pipelineRun.Labels["mintmaker.appstudio.redhat.com/component"]
	componentNamespace := pipelineRun.Labels["mintmaker.appstudio.redhat.com/namespace"]

	var pipelineIdentifier string
	if componentName != "" && componentNamespace != "" {
		component := &appstudiov1alpha1.Component{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      componentName,
			Namespace: componentNamespace,
		}, component); err == nil {
			pipelineIdentifier = fmt.Sprintf("%s/%s",
				component.Spec.Source.GitSource.URL,
				component.Spec.Source.GitSource.Revision)
		}
	}

	// Step 1: Check which step failed and overall pipeline status
	failedStep, pipelineSucceeded, failReason := checkPipelineStatus(ctx, clientset, podName, namespace)
	log.Info("Pipeline status",
		"succeeded", pipelineSucceeded,
		"failedStep", failedStep,
		"reason", failReason)

	// Step 2: Process logs if step-renovate ran
	var podDetails *doctor.PodDetails
	if failedStep == "" || failedStep == "step-renovate" {
		// Only process logs if renovate ran (or if we're not sure which step failed)
		if _, err := os.Stat(logFilePath); err == nil {
			log.Info("Processing log file", logFilePath)
			podDetails, err = processLogFile(logFilePath, failReason)
			if err != nil {
				log.Error(err, "Failed to process log file")
			}
		} else {
			log.Error(err, "Log file not found (step-renovate may not have run)", "path", logFilePath)
		}
	}

	// If no podDetails, create basic one with failure info
	if podDetails == nil {
		podDetails = &doctor.PodDetails{
			FailureLogs: buildFailureReason(failedStep, failReason),
		}
	}

	// Create Kite client
	kiteClient, _ := kite.NewClient(kiteAPIURL)
	kiteVer, err := kiteClient.GetVersion(ctx)
	if err != nil {
		log.Error(err, "Failed to get KITE API version", "stderr", os.Stderr, "apiURL", kiteAPIURL)
	}
	log.Info(fmt.Sprintf("Using KITE API version: %s", kiteVer))

	// Send custom webhooks (only if we have log analysis)
	if len(podDetails.Error) > 0 {
		sendCustomWebhooks(ctx, kiteClient, componentNamespace, pipelineIdentifier, podDetails)
	}

	// Always send success or failure webhook
	if pipelineSucceeded {
		sendSuccessWebhook(ctx, kiteClient, componentNamespace, pipelineIdentifier)
	} else {
		failureReason := podDetails.FailureLogs
		if failureReason == "" {
			failureReason = buildFailureReason(failedStep, failReason)
		}
		sendFailureWebhook(ctx, kiteClient, componentNamespace, pipelineIdentifier,
			pipelineRunName, failureReason)
	}

	log.Info("Successfully completed log analysis and webhook sending")
}

// createK8sClient creates a controller-runtime client with Tekton and Component schemes
func createK8sClient(cfg *rest.Config) (client.Client, error) {
	scheme := scheme.Scheme

	// Add core Kubernetes types
	_ = corev1.AddToScheme(scheme)

	// Add Tekton types
	_ = tektonv1.AddToScheme(scheme)

	// Add Component types
	_ = appstudiov1alpha1.AddToScheme(scheme)

	return client.New(cfg, client.Options{Scheme: scheme})
}

// checkPipelineStatus checks which step failed and overall pipeline status
func checkPipelineStatus(ctx context.Context, clientset *kubernetes.Clientset,
	podName, namespace string) (failedStep string, pipelineSucceeded bool, failReason string) {

	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "unknown", false, fmt.Sprintf("Failed to get pod: %v", err)
	}

	// Check status of each step container
	stepOrder := []string{"step-prepare-db", "step-prepare-rpm-cert", "step-renovate"}

	for _, stepName := range stepOrder {
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == stepName {
				if status.State.Terminated != nil {
					if status.State.Terminated.ExitCode != 0 {
						// This step failed
						return stepName, false, status.State.Terminated.Reason
					}
					// This step succeeded, continue checking
					break
				}
				// Still running or waiting - shouldn't happen at this point
				if status.State.Running != nil || status.State.Waiting != nil {
					return stepName, false, "Step still running"
				}
			}
		}
	}

	// Check step-renovate specifically for success
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "step-renovate" {
			if status.State.Terminated != nil {
				if status.State.Terminated.ExitCode == 0 {
					return "", true, ""
				}
				return "step-renovate", false, status.State.Terminated.Reason
			}
		}
	}

	// If we get here, couldn't determine status
	return "unknown", false, "Could not determine pipeline status"
}

// processLogFile reads and processes logs from file
func processLogFile(logFilePath, simpleReason string) (*doctor.PodDetails, error) {
	failMsg, report, err := doctor.ProcessLogFile(logFilePath, simpleReason)
	if err != nil {
		return nil, err
	}

	return &doctor.PodDetails{
		FailureLogs: failMsg,
		Error:       report.Errors,
		Warning:     report.Warnings,
		Info:        report.Infos,
	}, nil
}

// sendCustomWebhooks sends error, warning, and info webhooks
func sendCustomWebhooks(ctx context.Context, kiteClient *kite.Client, namespace, pipelineIdentifier string, podDetails *doctor.PodDetails) {
	if len(podDetails.Error) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "error", podDetails.Error); err != nil {
			log.Error(err, "Failed to send error webhook")
		}
	}
	if len(podDetails.Warning) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "warning", podDetails.Warning); err != nil {
			log.Error(err, "Failed to send warning webhook")
		}
	}
	if len(podDetails.Info) > 0 {
		if err := sendCustomWebhook(ctx, kiteClient, namespace, pipelineIdentifier, "info", podDetails.Info); err != nil {
			log.Error(err, "Failed to send info webhook")
		}
	}
}

func sendCustomWebhook(ctx context.Context, kiteClient *kite.Client, namespace, pipelineIdentifier, issueType string, logs []string) error {
	if len(logs) < 1 {
		return fmt.Errorf("found %d entries of type %s", len(logs), issueType)
	}

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

// buildFailureReason creates a descriptive failure reason
func buildFailureReason(failedStep, reason string) string {
	if failedStep == "" {
		return fmt.Sprintf("Pipeline failed: %s", reason)
	}

	switch failedStep {
	case "step-prepare-db":
		return fmt.Sprintf("Pipeline failed at prepare-db step: %s", reason)
	case "step-prepare-rpm-cert":
		return fmt.Sprintf("Pipeline failed at prepare-rpm-cert step: %s", reason)
	case "step-renovate":
		return fmt.Sprintf("Pipeline failed at renovate step: %s", reason)
	default:
		return fmt.Sprintf("Pipeline failed at %s: %s", failedStep, reason)
	}
}
