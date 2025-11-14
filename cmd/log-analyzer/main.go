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
	"path/filepath"
	"strings"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appstudiov1alpha1 "github.com/konflux-ci/application-api/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	"github.com/konflux-ci/renovate-log-analyzer/internal/pkg/doctor"
	"github.com/konflux-ci/renovate-log-analyzer/internal/pkg/kite"
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

	log = log.WithValues(
		"podName", podName,
	)

	// Now use the logger throughout your code
	log.Info("Starting log analyzer service")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize clients
	cfg, err := getKubernetesConfig()
	if err != nil {
		log.Error(err, "Failed to get Kubernetes config")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Error(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}

	k8sClient, err := createK8sClient(cfg)
	if err != nil {
		log.Error(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	// Get PipelineRun info
	pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get pod")
		os.Exit(1)
	}

	// Extract pipeline, component, git host, and repository information from the pod labels
	pipelineRunName := pod.Labels["tekton.dev/pipelineRun"]
	componentName := pod.Labels["mintmaker.appstudio.redhat.com/component"]
	componentNamespace := pod.Labels["mintmaker.appstudio.redhat.com/namespace"]
	gitHost := pod.Labels["mintmaker.appstudio.redhat.com/git-host"]
	repository := pod.Labels["mintmaker.appstudio.redhat.com/repository"]
	repository = strings.ReplaceAll(repository, "_", "/")
	log = log.WithValues(
		"pipelineRun", pipelineRunName,
		"component", componentName,
		"namespace", componentNamespace,
		"gitHost", gitHost,
		"repository", repository,
	)

	pipelineIdentifier := fmt.Sprintf("%s/%s",
		gitHost, repository)
	component := &appstudiov1alpha1.Component{}
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      componentName,
		Namespace: componentNamespace,
	}, component); err == nil {
		pipelineIdentifier = fmt.Sprintf("%s/%s",
			pipelineIdentifier, component.Spec.Source.GitSource.Revision)

		log = log.WithValues(
			"branch", component.Spec.Source.GitSource.Revision,
		)
	}

	// Step 1: Check which step failed and overall pipeline status
	failedStep, pipelineSucceeded, simpleMsg := checkPipelineStatus(pod)
	log.Info("Pipeline status",
		"succeeded", pipelineSucceeded,
		"failedStep", failedStep,
		"message", simpleMsg)

	// Step 2: Process logs if step-renovate ran
	var processedFailReason string
	processedFailReason, err = doctor.ProcessLogFile(logFilePath, simpleMsg)
	if err != nil {
		log.Error(err, "Failed to process logs")
	} else {
		log.Info("Successfully processed logs",
			"failureLogs", processedFailReason)
	}

	// Create Kite client
	kiteClient, err := kite.NewClient(kiteAPIURL)
	if err != nil {
		log.Error(err, "Failed to create Kite client", "apiURL", kiteAPIURL)
		os.Exit(1)
	}

	kiteStatus, err := kiteClient.GetKITEstatus(ctx)
	if err != nil {
		log.Error(err, "Request for KITE API status failed", "apiURL", kiteAPIURL)
		os.Exit(1)
	}
	log.Info("KITE API status request completed", "status", kiteStatus, "apiURL", kiteAPIURL)

	// Send success or failure webhook
	if pipelineSucceeded {
		if err := sendSuccessWebhook(ctx, kiteClient, componentNamespace, pipelineIdentifier); err != nil {
			log.Error(err, "Failed to send success webhook")
			os.Exit(1)
		}
		log.Info("Successfully sent success webhook")
	} else {
		if err := sendFailureWebhook(ctx, kiteClient, componentNamespace, pipelineIdentifier,
			pipelineRunName, processedFailReason); err != nil {
			log.Error(err, "Failed to send failure webhook")
			os.Exit(1)
		}
		log.Info("Successfully sent failure webhook", "failureMsg", processedFailReason)
	}

	log.Info("Successfully completed log analysis and sent webhook")
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
func checkPipelineStatus(pod *corev1.Pod) (failedStep string, pipelineSucceeded bool, failReason string) {

	// Check step-renovate specifically for success
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "step-renovate" {
			if status.State.Terminated != nil {
				// Check the actual exit code from Message field first (more reliable)
				actualExitCode := getExitCodeFromMessage(status.State.Terminated.Message)
				if actualExitCode != nil {
					if *actualExitCode == 0 {
						return "", true, ""
					}
					return "step-renovate", false, fmt.Sprintf("exit code = %d", *actualExitCode)
				}
				// Fallback to Terminated.ExitCode if Message doesn't have ExitCode
				if status.State.Terminated.ExitCode == 0 {
					return "", true, ""
				}
				return "step-renovate", false, fmt.Sprintf("exit code = %d", status.State.Terminated.ExitCode)
			}
		}
	}

	// If we get here, couldn't determine status
	return "unknown", false, "Could not determine pipeline status"
}

// getExitCodeFromMessage extracts the actual exit code from the Message field
// The Message field contains an array of objects with "key" and "value" fields
// We look for the one with key "ExitCode" and return its integer value
func getExitCodeFromMessage(message string) *int32 {
	if message == "" {
		return nil
	}

	// Parse the JSON array from the Message field
	var messages []map[string]interface{}
	if err := json.Unmarshal([]byte(message), &messages); err != nil {
		// If parsing fails, return nil (fallback to Terminated.ExitCode)
		return nil
	}

	// Find the ExitCode entry
	for _, msg := range messages {
		if key, ok := msg["key"].(string); ok && key == "ExitCode" {
			if value, ok := msg["value"].(string); ok {
				// Parse the string value as integer
				var exitCode int32
				if _, err := fmt.Sscanf(value, "%d", &exitCode); err == nil {
					return &exitCode
				}
			}
		}
	}

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

// for local testing only
// getKubernetesConfig gets Kubernetes config, trying in-cluster first, then kubeconfig
func getKubernetesConfig() (*rest.Config, error) {
	// Try in-cluster config first (when running in Kubernetes)
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}

	// Fallback to kubeconfig (for local development)
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	// Check if kubeconfig file exists
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		return nil, fmt.Errorf("neither in-cluster config nor kubeconfig found: %w", err)
	}

	cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return cfg, nil
}
