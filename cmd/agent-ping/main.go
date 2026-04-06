// Command agent-ping sends task notifications to an AgentNotify Router webhook.
//
// Usage:
//
//	agent-ping -task-id ID -project NAME -state STATE -source SRC -title TITLE [-details TEXT]
//
// Environment variables:
//
//	AGENT_PING_WEBHOOK_URL  (required) The Apps Script web app URL.
//	AGENT_PING_SECRET       (optional) Shared secret sent as auth_token in JSON body.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	agentping "github.com/jaeyeom/agent-ping"
)

func main() {
	taskID := flag.String("task-id", "", "Task ID (default: auto-generated UUID)")
	project := flag.String("project", "default", "Project name")
	state := flag.String("state", "", "Event state: start|waiting|completed|error (required)")
	source := flag.String("source", "cli", "Event source identifier")
	title := flag.String("title", "", "Short human-readable title (required)")
	details := flag.String("details", "", "Optional longer text")
	flag.Parse()

	webhookURL := os.Getenv("AGENT_PING_WEBHOOK_URL")
	if webhookURL == "" {
		fmt.Fprintln(os.Stderr, "error: AGENT_PING_WEBHOOK_URL environment variable is required")
		os.Exit(1)
	}

	if *state == "" || *title == "" {
		fmt.Fprintln(os.Stderr, "error: -state and -title are required")
		flag.Usage()
		os.Exit(1)
	}

	if *taskID == "" {
		*taskID = uuid.NewString()
	}

	client := agentping.NewClient(webhookURL)
	if secret := os.Getenv("AGENT_PING_SECRET"); secret != "" {
		client.Secret = secret
	}

	event := agentping.Event{
		TaskID:  *taskID,
		Project: *project,
		State:   agentping.State(*state),
		Source:  *source,
		Title:   *title,
		Details: *details,
	}

	resp, err := client.Send(context.Background(), event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ok task_id=%s thread_id=%s\n", resp.TaskID, resp.ThreadID)
}
