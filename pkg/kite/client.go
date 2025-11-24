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

package kite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type HealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type PipelineFailurePayload struct {
	PipelineName  string `json:"pipelineName"`
	Namespace     string `json:"namespace"`
	FailureReason string `json:"failureReason"`
	RunID         string `json:"runId,omitempty"`
	LogsURL       string `json:"logsUrl,omitempty"`
}

type PipelineSuccessPayload struct {
	PipelineName string `json:"pipelineName"`
	Namespace    string `json:"namespace"`
}

// NewClient creates a new Kite API client
func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("Kite API base URL cannot be empty")
	}

	_, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// sendRequest sends the given request to Kite API and stores
// the decoded response body in the value pointed to by out
func (c *Client) sendRequest(req *http.Request, out any) error {
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, readErr := io.ReadAll(resp.Body)

		responseBody := ""
		if readErr == nil {
			responseBody = string(bodyBytes)
		}
		return fmt.Errorf("Kite API returned status code %d: %s", resp.StatusCode, responseBody)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// GetVersion returns the Kite API version
func (c *Client) GetKiteStatus(ctx context.Context) (string, error) {
	// baseURL is already validated in NewClient, so this should never fail
	u, _ := url.Parse(c.baseURL)
	u.Path = path.Join(u.Path, "api/v1/health")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}

	var respBody HealthResponse
	if err := c.sendRequest(req, &respBody); err != nil {
		return "", err
	}

	statusStr := respBody.Status
	if statusStr == "" {
		statusStr = "unknown status"
	}

	messageStr := respBody.Message
	if messageStr == "" {
		messageStr = "unknown status detail"
	}

	return fmt.Sprintf("%s: %s", statusStr, messageStr), nil
}

// SendWebhookRequest creates the URL, adds the namespace to the query parameters,
// creates a request, and sends it to Kite API
func (c *Client) SendWebhookRequest(ctx context.Context, namespace string, webhookName string, payload []byte) error {
	// baseURL is already validated in NewClient, so this should never fail
	u, _ := url.Parse(c.baseURL)
	u.Path = path.Join(u.Path, "api/v1/webhooks", webhookName)

	q := u.Query()
	q.Set("namespace", namespace)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	return c.sendRequest(req, nil)
}
