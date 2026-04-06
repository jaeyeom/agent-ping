// Package agentping provides a client for sending task notifications to
// the AgentNotify Router (a Google Apps Script webhook that manages Gmail
// threads for agent task tracking).
package agentping

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// State represents the lifecycle state of a task.
type State string

const (
	StateStart     State = "start"
	StateWaiting   State = "waiting"
	StateCompleted State = "completed"
	StateError     State = "error"
)

// Event is the JSON payload sent to the AgentNotify Router.
type Event struct {
	TaskID    string `json:"task_id"`
	Project   string `json:"project"`
	State     State  `json:"state"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	Details   string `json:"details,omitempty"`
	Timestamp string `json:"timestamp"`
	AuthToken string `json:"auth_token,omitempty"`
}

// Response is the JSON response from the AgentNotify Router.
type Response struct {
	OK       bool   `json:"ok"`
	TaskKey  string `json:"task_key,omitempty"`
	TaskID   string `json:"task_id,omitempty"`
	State    string `json:"state,omitempty"`
	ThreadID string `json:"thread_id,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Client sends task events to an AgentNotify Router webhook.
type Client struct {
	WebhookURL string
	Secret     string // optional shared secret sent as auth_token in the JSON body
	HTTPClient *http.Client
}

// NewClient creates a Client for the given webhook URL.
func NewClient(webhookURL string) *Client {
	return &Client{
		WebhookURL: webhookURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Send posts an Event to the webhook and returns the router's response.
func (c *Client) Send(ctx context.Context, event Event) (*Response, error) {
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if c.Secret != "" {
		event.AuthToken = c.Secret
	}

	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result Response
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode response (status %d, body %q): %w", resp.StatusCode, string(respBody), err)
	}

	if !result.OK {
		return &result, fmt.Errorf("router error: %s", result.Error)
	}

	return &result, nil
}
