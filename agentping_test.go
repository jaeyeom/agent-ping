package agentping_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentping "github.com/jaeyeom/agent-ping"
)

func TestSend(t *testing.T) {
	var got agentping.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(agentping.Response{
			OK:       true,
			TaskID:   got.TaskID,
			TaskKey:  got.Project + ":" + got.TaskID,
			State:    string(got.State),
			ThreadID: "thread-abc",
		})
	}))
	defer server.Close()

	client := agentping.NewClient(server.URL)
	event := agentping.Event{
		TaskID:         "task-1",
		Project:        "test-project",
		State:          agentping.StateWaiting,
		Source:         "test",
		Title:          "Need approval",
		Details:        "Please review",
		Hostname:       "devbox",
		OS:             "Darwin",
		CWD:            "/tmp/test-project",
		SessionShortID: "task-1",
	}

	resp, err := client.Send(context.Background(), event)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.TaskID != "task-1" {
		t.Errorf("TaskID = %s, want task-1", got.TaskID)
	}
	if got.State != agentping.StateWaiting {
		t.Errorf("State = %s, want waiting", got.State)
	}
	if got.Hostname != "devbox" {
		t.Errorf("Hostname = %s, want devbox", got.Hostname)
	}
	if got.CWD != "/tmp/test-project" {
		t.Errorf("CWD = %s, want /tmp/test-project", got.CWD)
	}
	if got.SessionShortID != "task-1" {
		t.Errorf("SessionShortID = %s, want task-1", got.SessionShortID)
	}
	if got.Timestamp == "" {
		t.Error("Timestamp should be auto-filled")
	}
	if resp.ThreadID != "thread-abc" {
		t.Errorf("ThreadID = %s, want thread-abc", resp.ThreadID)
	}
}

func TestSendWithSecret(t *testing.T) {
	var got agentping.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(agentping.Response{OK: true, TaskID: "t1"})
	}))
	defer server.Close()

	client := agentping.NewClient(server.URL)
	client.Secret = "my-secret"

	_, err := client.Send(context.Background(), agentping.Event{
		TaskID:  "t1",
		Project: "p",
		State:   agentping.StateStart,
		Source:  "test",
		Title:   "test",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.AuthToken != "my-secret" {
		t.Errorf("auth_token = %q, want %q", got.AuthToken, "my-secret")
	}
}

func TestSendErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(agentping.Response{OK: false, Error: "Missing required field: state"})
	}))
	defer server.Close()

	client := agentping.NewClient(server.URL)
	_, err := client.Send(context.Background(), agentping.Event{
		TaskID:  "t1",
		Project: "p",
		State:   "bad",
		Source:  "test",
		Title:   "test",
	})
	if err == nil {
		t.Fatal("expected error for bad response")
	}
}
