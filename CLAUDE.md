# agent-ping

Gmail-based notification system for tracking AI agent task lifecycles.

## Architecture

- **Go client + CLI** (`agentping.go`, `cmd/agent-ping/`) — sends events via HTTP POST
- **Apps Script router** (`appscript/Code.gs`) — receives events, manages Gmail threads
- **Hook script** (`scripts/agent-ping-hook`) — integrates with Claude Code hooks

The Go client does NOT talk to Gmail directly. It only sends JSON to the Apps Script webhook, which handles all Gmail threading, labeling, and inbox management.

## Gotchas

- **Apps Script POST redirects**: Google Apps Script returns a 302 redirect on POST. Clients must follow the redirect as GET (not re-POST). The Go `net/http` client handles this correctly by default. With `curl`, use `-L` without `-X POST` — use `-d` to imply POST on the initial request only.
- **`appscript/Setup.gs` contains secrets**: It's in `.gitignore` for a reason. The deployed version has real `TARGET_EMAIL` and `SHARED_SECRET` values.
- **Apps Script deployment**: After editing `Code.gs`, you must create a **new deployment** (not just save) for changes to take effect at the `/exec` URL. "Test deployment" (`/dev`) uses the latest saved code but `/exec` is pinned to a deployment version.
- **Gmail threading requires stable subjects**: The canonical subject includes `task_id`, `project`, `source`, and `title` but never `state`. Putting state in the subject would break Gmail thread grouping.

## Environment Variables

| Variable | Used by | Required | Purpose |
|---|---|---|---|
| `AGENT_PING_WEBHOOK_URL` | CLI, hook script | Yes | Apps Script web app URL |
| `AGENT_PING_SECRET` | CLI, hook script | If `SHARED_SECRET` is set in Apps Script | Sent as `auth_token` in JSON body |
| `AGENT_PING_PROJECT` | Hook script | No | Override project name (default: basename of cwd) |
| `AGENT_PING_SOURCE` | Hook script | No | Override source identifier (default: `claude-code`) |

The CLI reads `AGENT_PING_WEBHOOK_URL` and `AGENT_PING_SECRET`. The hook script reads all four.

## Hook Integration

Claude Code hooks map to agent-ping states:

| Hook Event | State | Gmail Effect |
|---|---|---|
| `SessionStart` | `start` | Thread created/archived |
| `Stop` | `waiting` | Thread → inbox, unread |
| `UserPromptSubmit` | `start` | Thread archived |
| `SessionEnd` | `completed` | Thread stays archived |

The hook script deduplicates consecutive identical states per `session_id` via temp files in `$TMPDIR/agent-ping-state/`.
