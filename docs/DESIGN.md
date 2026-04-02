# Agent Notification System – Gmail Threaded Design

**Version:** 2.0
**Date:** March 31, 2026
**Author:** Design for multi‑agent task tracking via Gmail threads

***

## 1. Overview

*Defines the problem (tracking long-running agent tasks) and the solution approach (Gmail threads as a task board). Sets scope boundaries with goals and non‑goals.*

### Purpose

Provide a centralized, Gmail‑based notification system for tracking long‑running tasks across AI agents, terminals, and cloud services, where **each task maps to a single Gmail conversation**, and “resumed/completed” events **auto‑dismiss** the thread from the inbox.

### Goals

- Use **your Gmail inbox** as the “currently waiting on me” board.
- Keep **all messages for a task/session in one Gmail thread**.
- When a task resumes or completes, **archive that thread**, effectively auto‑dismissing the earlier “waiting for input” prompt.
- Support many heterogeneous sources (LLM agents, terminals, CI, cloud jobs) via a simple HTTP interface.
- Keep server logic minimal; rely on Gmail threads + filters for state visibility.

### Non‑Goals

- Multi‑channel routing (Slack/Discord) in v1.
- Complex reminder timers or SLAs (we only care about “waiting vs not waiting”).
- Multi‑user routing (v1 targets a single personal Gmail account).

***

## 2. Architecture

*Shows the system topology: who sends events, what processes them, and where state ends up. Answers "what are the moving parts and how do they connect?"*

### Components

```
┌────────────────────────────────────────────────────────┐
│ Event Sources:                                         │
│  - LLM agents (Claude Code, Perplexity Computer, etc.) │
│  - Terminals / CLIs                                    │
│  - CI / cloud services                                 │
└──────────────────┬─────────────────────────────────────┘
                   │ HTTP POST (JSON)
                   ▼
          ┌────────────────────────┐
          │ Google Apps Script     │
          │  "AgentNotify Router"  │
          └──────────┬─────────────┘
                     │
                     │ Uses Advanced Gmail Service (Gmail API)
                     ▼
         ┌─────────────────────────────┐
         │ Gmail threads (conversation │
         │ per task_id)                │
         └──────────┬──────────────────┘
                    │
                    │ Filters / labels / archive rules
                    ▼
           ┌───────────────────────────┐
           │ User’s Gmail UI           │
           │ - Inbox: waiting tasks    │
           │ - All Mail: history       │
           └───────────────────────────┘
```

### Data Flow

1. Event source POSTs JSON describing a task event (`task_id`, `state`, etc.) to Apps Script.
2. Router normalizes the event and:
   - Finds or creates the **Gmail thread** for that `task_id`.
   - Appends a **reply** in that thread with canonical subject and content.
   - Optionally **archives** the thread depending on state.
3. Gmail filters and labels organize threads; inbox shows only tasks in `waiting` / `error` states.

***

## 3. Event Model

*Specifies the contract between event producers and the router. Defines the four states and the exact JSON schema. This is the API reference for anyone writing a client.*

### Event States

Four core states, from the router’s point of view:

- `start` – task has started or resumed; does **not** require user action.
- `waiting` – task is waiting on the user; must be **visible in inbox**.
- `completed` – task finished; no action required; thread should be archived.
- `error` – task failed; usually requires attention; treat like `waiting` (visible), or separate label.

### JSON Payload

```json
{
  "task_id": "uuid-or-stable-id",
  "project": "project-name",
  "state": "start|waiting|completed|error",
  "source": "agent-or-service-name",
  "title": "Short human-readable description",
  "details": "Optional longer text (logs, prompts, errors)",
  "timestamp": "2026-03-31T20:53:00Z"
}
```

**Fields:**

- `task_id`: Unique per logical task/session. *Critical for mapping to a Gmail thread.*
- `project`: Logical project/workspace name.
- `state`: One of the four states above.
- `source`: Agent/service identifier (`claude-code`, `perplexity-computer`, `ci-pipeline`, etc.).
- `title`: Human‑readable summary, appears in subject.
- `details`: Arbitrary text: prompt, logs, error details, links.
- `timestamp`: ISO8601 UTC; used for debugging and human context.

***

## 4. Gmail Threading Strategy

*Explains how multiple events for one task stay in a single Gmail conversation. Covers the mechanics (subject stability, `threadId` reuse) and the persistent state needed to make it work.*

### Requirements

- **One Gmail conversation per `task_id`.**
- First event for a task creates a new thread.
- Subsequent events for that `task_id` are **replies in the same thread**.
- “Resumed” (`start`) and “completed” events **archive** the thread.

### Threading Mechanics

Gmail groups messages into conversations based on **subject** and headers like `In-Reply-To`, `References`, and internal `threadId`. Google’s Gmail API allows explicitly sending into an existing thread via `threadId`. [support.google](https://support.google.com/mail/answer/5900?hl=en&co=GENIE.Platform%3DDesktop)

We will:

- Use a **canonical subject** per task (includes `task_id`, `project`, etc.).
- On the **first event**:
  - Send a new message.
  - Capture the Gmail `threadId` and root `Message-ID`.
- On subsequent events:
  - Send a reply with `threadId` using Gmail API / Advanced Gmail Service so Gmail keeps them in the same conversation. [developers.google](https://developers.google.com/workspace/gmail/api/guides/threads)

This requires a small piece of persistent state keyed by `task_id` → `threadId` (and optionally the root Message‑ID for advanced header control).

***

## 5. Email Conventions

*Prescribes the exact format of emails: recipient address, subject line template, and body template. Ensures consistency so threading doesn't break and emails are searchable.*

### Destination

- Use a **single fixed `To:` address** for all events:
  - Either `yourname@gmail.com` or `yourname+agentnotify@gmail.com`.
- Do **not** vary the `To:` based on state or project; changing recipients can cause Gmail to split threads. [streak](https://www.streak.com/post/gmail-plus-addressing-trick)

### Subject

Canonical subject format (stable across the task lifetime):

```text
[project][source][task:TASK_ID] TITLE
```

Example:

```text
[backend-service][claude-code][task:db-migration-123] Approve production schema change
```

Notes:

- **State is NOT included in the subject.** Gmail requires a matching subject to keep messages in the same thread. Changing the subject (e.g., from `[waiting]` to `[start]`) would break threading. State goes in the body and labels instead.
- `task:...` tag makes tasks searchable.

### Body

Body template (plain text is fine):

```text
Project: {project}
Task ID: {task_id}
Source: {source}
State: {state}
Timestamp: {timestamp}

{title}

---
{details or "(no additional details)"}
---

Task ID: {task_id}
```

We don’t need to manually manage `In-Reply-To`/`References` headers in Apps Script if we use the Gmail API’s `threadId` parameter correctly, but the body repeats key info for search and resilience. [github](https://github.com/n8n-io/n8n/issues/15775)

***

## 6. Gmail Labeling & Auto‑Dismiss Semantics

*Defines the inbox/archive behavior per state. This is the core UX logic: `waiting` → inbox, `start`/`completed` → archive. Also covers label taxonomy.*

### Labels

Create:

- `AI/All` – all agent threads, regardless of state.
- `AI/Waiting` – threads currently waiting on user.
- `AI/Error` – threads in error (optional).
- `AI/Archive` – everything not currently waiting (optional; the archive itself is All Mail).
- Optional: per‑project labels (`AI/Project/backend-service`, etc.).

### Core Behavior

- `waiting` / `error` events:
  - Thread must be **in Inbox** (visible).
  - Apply `AI/Waiting` (and optionally `AI/Error`).
- `start` / `completed` events:
  - After appending reply, **archive the thread** (remove from Inbox but keep in All Mail).
  - Optionally remove `AI/Waiting` label.

The net effect:

- Inbox shows “things currently waiting on me”.
- When an LLM resumes work or finishes, a new reply arrives and the router archives that thread, effectively auto‑dismissing the waiting prompt.

### Filter Strategy

Gmail filters will operate on:

- Sender (Apps Script / router address).
- Subject tokens (`[project]`, `[source]`, `[state]`, `[task:...]`).
- Labels can be set by the router via GmailThread APIs as well.

We will *not* rely on plus‑aliases per state, to avoid fragmenting threads. [support.google](https://support.google.com/mail/answer/5900?hl=en&co=GENIE.Platform%3DDesktop)

***

## 7. Router Implementation (Apps Script + Gmail Advanced Service)

*Implementation guide for the server-side component. Sections 3–6 define what the system should do; this section tells a developer how to build it in Apps Script. Covers storage for the `task_id` → `threadId` mapping, step-by-step control flow for each incoming POST, and which Apps Script / Gmail API methods to call. Bridges the abstract design to the concrete code in `DESIGN-APPS-SCRIPT.md`.*

### Storage Requirements

We need to persist:

- `task_id`
- `threadId` (Gmail thread ID)
- Optional: `rootMessageId`
- Optional: last known state (for debugging)

Given Apps Script constraints, simplest is **Script Properties** or a bound Sheet:

- Key: `task:PROJECT:TASK_ID`
- Value: JSON containing `threadId` and metadata.

### Pseudocode Flow

For each POST event:

1. Parse JSON and validate required fields.
2. Compute `taskKey = project + ":" + task_id` (or just `task_id` if globally unique).
3. Look up existing entry in storage:
   - If found: use stored `threadId`.
   - If not found:
     - Create new message via Gmail API; record new `threadId`.
4. Create an email message:
   - `to` = your Gmail.
   - `subject` = canonical subject.
   - `body` = template body.
   - If `threadId` exists, send as part of that thread.
5. After sending, apply logic:
   - If state is `waiting` or `error`:
     - Ensure thread is **in Inbox**.
     - Add `AI/Waiting` label (and `AI/Error` if relevant).
   - If state is `start` or `completed`:
     - Remove `AI/Waiting` label if present.
     - Archive the thread (remove from Inbox).

N.B. Thread operations and labels are done using `GmailApp` / `GmailThread` / Gmail Advanced Service. [developers.google](https://developers.google.com/apps-script/reference/gmail/gmail-app)

### Apps Script: High‑Level Sketch

The coding agent should:

1. Enable **Advanced Gmail Service** (Services → Gmail API).
2. Use a Web App deployment with `doPost(e)` to accept JSON.
3. Implement helper functions:

- `getOrCreateThread(taskKey, subject, body)`:
  - If `threadId` known → return thread.
  - Else:
    - Send initial message with `Gmail.Users.Messages.send` (Advanced Gmail Service) without `threadId`.
    - Record returned `threadId`.
- `appendToThread(threadId, subject, body)`:
  - Use `Gmail.Users.Messages.send` with `threadId` set. [developers.google](https://developers.google.com/workspace/gmail/api/guides/threads)
- `updateThreadLabelsAndInbox(threadId, state)`:
  - Use `GmailApp.getThreadById(threadId)` and:
    - `addLabel/removeLabel`
    - `moveToInbox` or `moveToArchive`.

**Developer docs that show these primitives:** Gmail threads and labels via Apps Script / Gmail API. [developers.google](https://developers.google.com/apps-script/reference/gmail/gmail-thread)

***

## 8. Client Integration Examples

*Copy-paste-ready code for event producers in bash, Python, and Go. Lowers the adoption barrier for anyone integrating a new agent or service.*

Agents just POST JSON; they don’t need to know about Gmail threading.

### Bash/CLI Script

```bash
#!/bin/bash
# notify.sh - Send task notification to webhook

WEBHOOK_URL=”https://script.google.com/macros/s/YOUR_DEPLOYMENT_ID/exec”
TASK_ID=”${1:-$(uuidgen)}”
PROJECT=”${2:-default}”
STATE=”${3:-waiting}”
SOURCE=”${4:-cli}”
TITLE=”${5:-Task event}”
DETAILS=”${6:-}”

curl -X POST “$WEBHOOK_URL” \
  -H “Content-Type: application/json” \
  -d @- <<EOF
{
  “task_id”: “$TASK_ID”,
  “project”: “$PROJECT”,
  “state”: “$STATE”,
  “source”: “$SOURCE”,
  “title”: “$TITLE”,
  “details”: “$DETAILS”,
  “timestamp”: “$(date -u +%Y-%m-%dT%H:%M:%SZ)”
}
EOF
```

**Usage:**
```bash
# Task starts
./notify.sh “task-123” “backend-service” “start” “claude-code” “Running database migration”

# Task waiting
./notify.sh “task-123” “backend-service” “waiting” “claude-code” “Approve schema changes?” “Review the migration plan at...”

# User responds, task resumes
./notify.sh “task-123” “backend-service” “start” “claude-code” “Applying migration”

# Task completes
./notify.sh “task-123” “backend-service” “completed” “claude-code” “Migration successful”
```

### Python Client

```python
import requests
import uuid
from datetime import datetime, timezone

WEBHOOK_URL = “https://script.google.com/macros/s/YOUR_DEPLOYMENT_ID/exec”

def send_notification(task_id, project, state, source, title, details=””):
    payload = {
        “task_id”: task_id,
        “project”: project,
        “state”: state,
        “source”: source,
        “title”: title,
        “details”: details,
        “timestamp”: datetime.now(timezone.utc).isoformat()
    }

    response = requests.post(WEBHOOK_URL, json=payload)
    return response.json()

# Usage
task_id = str(uuid.uuid4())
send_notification(task_id, “data-pipeline”, “start”, “airflow”, “ETL job started”)
send_notification(task_id, “data-pipeline”, “waiting”, “airflow”, “Data quality check failed”, “See logs at...”)
send_notification(task_id, “data-pipeline”, “completed”, “airflow”, “ETL completed successfully”)
```

### Go Client

```go
package main

import (
    “bytes”
    “encoding/json”
    “net/http”
    “time”
)

const webhookURL = “https://script.google.com/macros/s/YOUR_DEPLOYMENT_ID/exec”

type Event struct {
    TaskID    string `json:”task_id”`
    Project   string `json:”project”`
    State     string `json:”state”`
    Source    string `json:”source”`
    Title     string `json:”title”`
    Details   string `json:”details,omitempty”`
    Timestamp string `json:”timestamp”`
}

func SendNotification(taskID, project, state, source, title, details string) error {
    event := Event{
        TaskID:    taskID,
        Project:   project,
        State:     state,
        Source:    source,
        Title:     title,
        Details:   details,
        Timestamp: time.Now().UTC().Format(time.RFC3339),
    }

    body, err := json.Marshal(event)
    if err != nil {
        return err
    }

    resp, err := http.Post(webhookURL, “application/json”, bytes.NewBuffer(body))
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return nil
}
```

### Integration with AI Agents

Agents that can run shell commands (Claude Code, Perplexity Computer, etc.) can use the bash script or invoke curl directly:

```bash
./notify.sh “$(uuidgen)” “my-project” “waiting” “claude-code” “Ready to deploy?” “Review changes and approve”
```

- LLM “waiting for user input” → `state=waiting`.
- After user approves and agent resumes, agent sends `state=start` for the same `task_id`.
- Router appends a reply and archives the thread.

***

## 9. UX Examples

*Walks through a concrete task lifecycle from the user's perspective. Makes the abstract state machine tangible: "here's what your inbox looks like at each step."*

### Example Thread Lifecycle

Task `db-migration-123`:

1. Agent starts:
   - `state=start` → router creates thread, appends message, archives thread (not visible, but queryable).
2. Agent needs approval:
   - `state=waiting` → router appends message, moves thread to Inbox, labels `AI/Waiting`. You see it as “Approve production schema change”.
3. You approve via some UI / CLI; agent resumes:
   - `state=start` → router appends message “Resuming migration…”, removes `AI/Waiting`, archives thread. Inbox no longer shows that task.
4. Agent completes:
   - `state=completed` → router appends message, thread remains archived.

Inbox always represents **current outstanding work**.

***

## 10. Security & Auth

*Covers how the webhook is protected. Minimal in v1 (shared secret), but establishes the threat model.*

Same as before, with one critical addition: API access.

- Webhook is a Script Web App URL; protect it via:
  - Shared secret token in header or payload.
  - Optionally IP allowlisting upstream (if using a proxy).
- Gmail API usage is internal (Apps Script has direct access to your Gmail).
- No external recipients; all mail goes to your Gmail.

***

## 11. Testing Plan

*Concrete curl commands to verify each state transition. Doubles as a manual acceptance test suite.*

### Manual Testing

1. **Deploy Apps Script:** verify GET endpoint returns a JSON status message.
2. **Send test “start” event:**
   ```bash
   curl -X POST “$WEBHOOK_URL” \
     -H “Content-Type: application/json” \
     -d '{“task_id”:”test-1”,”project”:”test-project”,”state”:”start”,”source”:”manual”,”title”:”Test task started”,”details”:”Testing thread creation”,”timestamp”:”2026-03-31T10:00:00Z”}'
   ```
3. **Check Gmail:** email should arrive, be archived (not in inbox), and have `AI/All` and `AI/Project/test-project` labels.
4. **Send “waiting” event for the same `task_id`:**
   ```bash
   curl -X POST “$WEBHOOK_URL” \
     -H “Content-Type: application/json” \
     -d '{“task_id”:”test-1”,”project”:”test-project”,”state”:”waiting”,”source”:”manual”,”title”:”Test task started”,”details”:”Waiting for approval”,”timestamp”:”2026-03-31T10:01:00Z”}'
   ```
5. **Check Gmail:** message should appear **in the same thread**, thread should be in inbox with `AI/Waiting` label.
6. **Send “start” event (resume):**
   ```bash
   curl -X POST “$WEBHOOK_URL” \
     -H “Content-Type: application/json” \
     -d '{“task_id”:”test-1”,”project”:”test-project”,”state”:”start”,”source”:”manual”,”title”:”Test task started”,”details”:”Resuming work”,”timestamp”:”2026-03-31T10:02:00Z”}'
   ```
7. **Check Gmail:** thread should be archived (auto-dismissed from inbox), `AI/Waiting` label removed.
8. **Send “completed” event:**
   ```bash
   curl -X POST “$WEBHOOK_URL” \
     -H “Content-Type: application/json” \
     -d '{“task_id”:”test-1”,”project”:”test-project”,”state”:”completed”,”source”:”manual”,”title”:”Test task started”,”details”:”All done”,”timestamp”:”2026-03-31T10:03:00Z”}'
   ```
9. **Check Gmail:** thread remains archived with all 4 messages in one conversation.

### Verification Checklist

- [ ] All messages for the same `task_id` land in a single Gmail thread
- [ ] `waiting` and `error` events move the thread to inbox
- [ ] `start` and `completed` events archive the thread (auto-dismiss)
- [ ] Labels (`AI/All`, `AI/Waiting`, `AI/Error`, `AI/Project/*`) are applied and removed correctly
- [ ] Invalid payloads (missing fields, bad state) return error responses
- [ ] Authentication rejects requests without a valid token (if `SHARED_SECRET` is set)

### Automated Testing (Future)

- Script that sends a full lifecycle sequence and verifies thread state via Gmail API
- Integration into CI to ensure the webhook remains functional after changes

***

## 12. Future Enhancements

*Prioritized roadmap (short/medium/long term) so readers know what's intentionally deferred vs. forgotten.*

### Short Term
- **Deduplication:** Add `event_id` field to suppress duplicate notifications from flaky callers
- **Debug endpoint:** `GET ?task_id=...&project=...` to inspect thread metadata
- **HTML email:** Rich formatting with color-coded states, clickable links
- **Per-project labels:** Automatically created via `getOrCreateLabel_` (already in Apps Script implementation)

### Medium Term
- **Slack/Discord integration:** Apps Script can POST to Slack/Discord webhooks in parallel with Gmail
- **Dashboard:** Simple HTML page showing current task statuses by querying Gmail API
- **Migrate metadata to Google Sheet:** Script Properties work for small scale, but a Sheet provides better visibility and debugging for higher volume
- **Reminders:** Time-based triggers in Apps Script to re-send “waiting” notifications if no follow-up event received

### Long Term
- **Migrate to dedicated service:** If Apps Script quotas become limiting, move to Cloud Functions, Cloud Run, or a Go service
- **State tracking database:** Firestore or Postgres for task lifecycle tracking, richer queries and analytics
- **Multi-user support:** Team notifications with user-specific routing and permissions

***

## 13. Quick Start Checklist

*Step-by-step deployment guide. Converts the full document into an actionable recipe.*

- [ ] Create new Google Apps Script project (“AgentNotify Router”)
- [ ] Enable **Advanced Gmail Service** (Services → Gmail API)
- [ ] Paste implementation from `DESIGN-APPS-SCRIPT.md` into `Code.gs`
- [ ] Set Script Properties: `TARGET_EMAIL` (required), `SHARED_SECRET` (optional)
- [ ] Deploy as Web App (Execute as: Me, Access: Anyone with the link)
- [ ] Copy the deployed web app URL
- [ ] Test with curl: send `start` → `waiting` → `start` → `completed` sequence
- [ ] Verify: single thread, auto-dismiss on resume/complete, labels applied
- [ ] Create client wrapper script (bash/python/go) with your webhook URL
- [ ] Integrate into agent/terminal/CI workflows
- [ ] Monitor inbox — only “waiting” and “error” tasks should be visible
