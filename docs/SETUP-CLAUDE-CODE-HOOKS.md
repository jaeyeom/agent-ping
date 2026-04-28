# Setting Up Claude Code Hooks for Agent Ping

This guide configures Claude Code to automatically send lifecycle
notifications to the AgentNotify Router so your Gmail inbox tracks
when sessions are working, waiting, or done.

## Prerequisites

1. **Apps Script deployed** — `WEB_APP_URL` returns `{"ok": true, ...}` on GET.
2. **Script properties set** — `TARGET_EMAIL` and `SHARED_SECRET` configured.
3. **`jq` installed** — hooks parse JSON from stdin.
4. **`agent-ping-hook` on PATH** — the wrapper script from `scripts/`.

## 1. Install the hook script

```bash
# From the repo root:
chmod +x scripts/agent-ping-hook

# Option A: symlink to a directory on PATH
ln -s "$(pwd)/scripts/agent-ping-hook" ~/bin/agent-ping-hook

# Option B: copy it
cp scripts/agent-ping-hook /usr/local/bin/
```

## 2. Set environment variables

Add these to your shell profile (`~/.zshrc`, `~/.bashrc`, or `.envrc`):

```bash
export AGENT_PING_WEBHOOK_URL="https://script.google.com/macros/s/YOUR_DEPLOYMENT_ID/exec"
export AGENT_PING_SECRET="your-shared-secret"

# Optional overrides:
# export AGENT_PING_PROJECT="my-project"   # default: basename of cwd
# export AGENT_PING_SOURCE="claude-code"   # default: claude-code
# export AGENT_PING_HOSTNAME="macbook-pro" # default: short system hostname
# export AGENT_PING_OS="Darwin"            # default: uname -s
# export AGENT_PING_INCLUDE_PROMPT="1"     # opt in to send a prompt preview
```

## 3. Configure Claude Code hooks

Add to your **project** (`.claude/settings.json`) or **user**
(`~/.claude/settings.json`) settings:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "agent-ping-hook start"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "agent-ping-hook waiting"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "agent-ping-hook start"
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "agent-ping-hook completed"
          }
        ]
      }
    ]
  }
}
```

## 4. How it works

The hooks map to agent-ping states:

```
Claude Code Event        agent-ping State    Gmail Effect
─────────────────────    ────────────────    ───────────────────
SessionStart          →  start              Thread created, archived
  (agent works...)
Stop                  →  waiting            Thread → inbox, unread
  (user reads inbox, responds...)
UserPromptSubmit      →  start              Thread archived again
  (agent resumes...)
Stop                  →  waiting            Thread → inbox, unread
  (user responds...)
UserPromptSubmit      →  start              Thread archived
  (agent finishes...)
SessionEnd            →  completed          Thread stays archived
```

**Deduplication:** The hook script tracks the last state sent per
`session_id` in a temp file. Back-to-back identical states (e.g.,
`SessionStart` → `start` then `UserPromptSubmit` → `start`) are
deduplicated — only the first is sent.

**Session context:** The hook sends `hostname`, `os`, `cwd`, and a
shortened `session_short_id` so the Gmail subject/body can identify
which machine and checkout a notification came from.

**Prompt previews:** If `AGENT_PING_INCLUDE_PROMPT=1`, the hook looks
for a prompt field in the hook JSON and sends it as `details`.
Enable this only if you are comfortable with prompt text being copied
into email.

**Short timeout:** The webhook call is synchronous, but `curl` uses a
10-second timeout so hooks fail fast instead of hanging the session.

**Safe to omit:** If `AGENT_PING_WEBHOOK_URL` is not set, the hook
exits silently. This means hooks can be committed to the repo without
breaking anything for team members who haven't configured the webhook.

## 5. Verify

Start a Claude Code session and check your Gmail:

1. Session starts → thread appears under `AI/All`, archived.
2. Claude finishes a turn → thread moves to inbox with `AI/Waiting`.
3. You type a response → thread archived again.
4. Exit the session → final "completed" message, thread stays archived.

The subject should now look roughly like:

```text
[agent-ping][claude-code@macbook-pro][task:abc12345] Agent working
```

And the body should include the full working directory plus any
optional prompt preview.

You can also check the state tracking files:

```bash
ls ${TMPDIR:-/tmp}/agent-ping-state/
```

## 6. Troubleshooting

**No emails arriving:**
- Verify: `curl -s -L "$AGENT_PING_WEBHOOK_URL"` returns `{"ok": true, ...}`
- Check that `AGENT_PING_SECRET` matches the Apps Script `SHARED_SECRET`
- Run the hook manually: `echo '{"session_id":"test-123","cwd":"/tmp"}' | agent-ping-hook start`

**Prompt text should not be emailed:**
- Unset `AGENT_PING_INCLUDE_PROMPT` or set it to `0`
- Re-run a prompt submit event and confirm the email body falls back to `(no additional details)`

**Duplicate emails:**
- Check `${TMPDIR:-/tmp}/agent-ping-state/` for stale state files
- Clear them: `rm ${TMPDIR:-/tmp}/agent-ping-state/*`

**Hook errors in Claude Code:**
- Hooks write errors to stderr. Check Claude Code's output for messages.
- The script uses `set -euo pipefail` — missing `jq` or env vars will surface clearly.
