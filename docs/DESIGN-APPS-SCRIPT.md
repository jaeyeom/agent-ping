# Apps Script Design

The key implementation points are:

- Use the **Advanced Gmail Service** to send raw RFC 2822 messages and attach follow-ups to an existing `threadId`. Google’s Gmail API requires `threadId`, matching `Subject`, and proper `References` / `In-Reply-To` headers to add a message to an existing thread. [developers.google](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages)
- Use Apps Script `GmailThread` methods like `moveToArchive()`, `moveToInbox()`, `addLabel()`, and `removeLabel()` to implement the “waiting visible / resumed auto-dismiss” behavior. [developers.google](https://developers.google.com/apps-script/reference/gmail)
- The first sent message can return a Gmail `threadId`, and the sent message headers can be fetched to capture the RFC `Message-ID` for future replies. [developers.google](https://developers.google.com/apps-script/reference/gmail/gmail-message)

## Apps Script skeleton

```javascript
/**
 * AgentNotify Router
 *
 * Requirements:
 * 1. Enable Advanced Gmail Service in Apps Script:
 *    Services -> + -> Gmail API
 * 2. Deploy as Web App.
 * 3. Set script properties:
 *    - TARGET_EMAIL = your Gmail address
 *    - SHARED_SECRET = optional bearer token / webhook secret
 *
 * Notes:
 * - This skeleton is intentionally explicit and conservative.
 * - It stores task -> thread metadata in Script Properties.
 * - It uses Gmail API raw messages for reliable threading.
 */

const CONFIG = {
  LABEL_ALL: 'AI/All',
  LABEL_WAITING: 'AI/Waiting',
  LABEL_ERROR: 'AI/Error',
  FROM_NAME: 'AgentNotify',
  PROPERTY_PREFIX: 'taskmeta:',
};

function doGet() {
  return jsonResponse({
    ok: true,
    service: 'AgentNotify Router',
    timestamp: new Date().toISOString(),
  });
}

function doPost(e) {
  try {
    authorizeRequest_(e);

    const payload = parseJsonBody_(e);
    validatePayload_(payload);

    const taskKey = buildTaskKey_(payload);
    const targetEmail = getRequiredScriptProperty_('TARGET_EMAIL');

    const canonicalSubject = buildCanonicalSubject_(payload);
    const plainBody = buildPlainBody_(payload);

    let meta = loadTaskMeta_(taskKey);

    if (!meta) {
      meta = createRootThread_({
        to: targetEmail,
        subject: canonicalSubject,
        body: plainBody,
        payload,
      });

      meta.taskKey = taskKey;
      meta.taskId = payload.task_id;
      meta.project = payload.project;
      meta.source = payload.source;
      meta.canonicalSubject = canonicalSubject;
      meta.lastState = payload.state;
      meta.updatedAt = new Date().toISOString();

      saveTaskMeta_(taskKey, meta);
    } else {
      const replyResult = appendToThread_({
        to: targetEmail,
        subject: meta.canonicalSubject || canonicalSubject,
        body: plainBody,
        threadId: meta.threadId,
        parentRfcMessageId: meta.lastRfcMessageId || meta.rootRfcMessageId,
      });

      meta.lastMessageId = replyResult.gmailMessageId;
      meta.lastRfcMessageId = replyResult.rfcMessageId;
      meta.lastState = payload.state;
      meta.updatedAt = new Date().toISOString();

      saveTaskMeta_(taskKey, meta);
    }

    applyThreadState_({
      threadId: meta.threadId,
      state: payload.state,
      project: payload.project,
    });

    return jsonResponse({
      ok: true,
      task_key: taskKey,
      task_id: payload.task_id,
      state: payload.state,
      thread_id: meta.threadId,
      subject: meta.canonicalSubject,
    });
  } catch (err) {
    return jsonResponse(
      {
        ok: false,
        error: String(err && err.message ? err.message : err),
        stack: err && err.stack ? err.stack : null,
      },
      400
    );
  }
}

/**
 * Creates the first/root message for a task thread.
 * Returns thread metadata including threadId and RFC Message-ID.
 */
function createRootThread_({ to, subject, body, payload }) {
  const raw = buildRawEmail_({
    to,
    subject,
    body,
  });

  const sent = Gmail.Users.Messages.send(
    {
      raw: raw,
    },
    'me'
  );

  const gmailMessageId = sent.id;
  const threadId = sent.threadId;

  const rfcMessageId = fetchRfcMessageId_(gmailMessageId);

  const meta = {
    threadId,
    rootMessageId: gmailMessageId,
    rootRfcMessageId: rfcMessageId,
    lastMessageId: gmailMessageId,
    lastRfcMessageId: rfcMessageId,
    createdAt: new Date().toISOString(),
    canonicalSubject: subject,
    lastState: payload.state,
  };

  return meta;
}

/**
 * Appends a follow-up message to an existing Gmail thread.
 * Gmail API threading rules require:
 * - same subject
 * - threadId
 * - RFC-compliant In-Reply-To / References headers
 */
function appendToThread_({ to, subject, body, threadId, parentRfcMessageId }) {
  const raw = buildRawEmail_({
    to,
    subject,
    body,
    inReplyTo: parentRfcMessageId,
    references: parentRfcMessageId,
  });

  const sent = Gmail.Users.Messages.send(
    {
      threadId: threadId,
      raw: raw,
    },
    'me'
  );

  const gmailMessageId = sent.id;
  const newThreadId = sent.threadId;
  const rfcMessageId = fetchRfcMessageId_(gmailMessageId);

  return {
    gmailMessageId,
    threadId: newThreadId,
    rfcMessageId,
  };
}

/**
 * Implements the core product UX:
 * - waiting/error -> thread visible in Inbox
 * - start/completed -> thread archived (auto-dismiss)
 */
function applyThreadState_({ threadId, state, project }) {
  const thread = GmailApp.getThreadById(threadId);
  if (!thread) {
    throw new Error(`Thread not found for threadId=${threadId}`);
  }

  const allLabel = getOrCreateLabel_(CONFIG.LABEL_ALL);
  const waitingLabel = getOrCreateLabel_(CONFIG.LABEL_WAITING);
  const errorLabel = getOrCreateLabel_(CONFIG.LABEL_ERROR);
  const projectLabel = getOrCreateLabel_(`AI/Project/${project}`);

  thread.addLabel(allLabel);
  thread.addLabel(projectLabel);

  if (state === 'waiting') {
    thread.addLabel(waitingLabel);
    thread.removeLabel(errorLabel);
    thread.moveToInbox();
    thread.markUnread();
    return;
  }

  if (state === 'error') {
    thread.addLabel(waitingLabel);
    thread.addLabel(errorLabel);
    thread.moveToInbox();
    thread.markUnread();
    return;
  }

  if (state === 'start' || state === 'completed') {
    safeRemoveLabel_(thread, waitingLabel);
    safeRemoveLabel_(thread, errorLabel);
    thread.markRead();
    thread.moveToArchive();
    return;
  }

  throw new Error(`Unsupported state for applyThreadState_: ${state}`);
}

/**
 * Payload validation
 */
function validatePayload_(payload) {
  const required = ['task_id', 'project', 'state', 'source', 'title', 'timestamp'];
  required.forEach((field) => {
    if (!payload[field]) {
      throw new Error(`Missing required field: ${field}`);
    }
  });

  const allowedStates = ['start', 'waiting', 'completed', 'error'];
  if (!allowedStates.includes(payload.state)) {
    throw new Error(`Invalid state: ${payload.state}`);
  }
}

/**
 * Subject must remain stable for the lifetime of the task/thread.
 * Important: do not include mutable state in the canonical subject if you want
 * maximum threading reliability.
 *
 * Recommended: keep state in body, not in the subject.
 */
function buildCanonicalSubject_(payload) {
  return `[${payload.project}][${payload.source}][task:${payload.task_id}] ${payload.title}`;
}

/**
 * Include state in the body so the thread stays stable even as state changes.
 */
function buildPlainBody_(payload) {
  return [
    `Project: ${payload.project}`,
    `Task ID: ${payload.task_id}`,
    `Source: ${payload.source}`,
    `State: ${payload.state}`,
    `Timestamp: ${payload.timestamp}`,
    '',
    payload.title,
    '',
    '---',
    payload.details || '(no additional details)',
    '---',
    '',
    `Task ID: ${payload.task_id}`,
  ].join('\n');
}

/**
 * Build base64url-encoded raw RFC 2822 email for Gmail API.
 */
function buildRawEmail_({ to, subject, body, inReplyTo, references }) {
  const fromEmail = Session.getActiveUser().getEmail();
  const headers = [
    `To: ${to}`,
    `From: ${CONFIG.FROM_NAME} <${fromEmail}>`,
    `Subject: ${subject}`,
    'MIME-Version: 1.0',
    'Content-Type: text/plain; charset=UTF-8',
    'Content-Transfer-Encoding: 7bit',
  ];

  if (inReplyTo) {
    headers.push(`In-Reply-To: ${inReplyTo}`);
  }

  if (references) {
    headers.push(`References: ${references}`);
  }

  const message = headers.join('\r\n') + '\r\n\r\n' + body;
  return Utilities.base64EncodeWebSafe(message, Utilities.Charset.UTF_8);
}

/**
 * Fetches the RFC Message-ID header from a sent Gmail message.
 */
function fetchRfcMessageId_(gmailMessageId) {
  const message = Gmail.Users.Messages.get('me', gmailMessageId, {
    format: 'metadata',
    metadataHeaders: ['Message-ID', 'Message-Id'],
  });

  const headers = (((message || {}).payload || {}).headers) || [];
  const found = headers.find(
    (h) => h.name && h.value && h.name.toLowerCase() === 'message-id'
  );

  if (!found) {
    throw new Error(`Could not find RFC Message-ID for gmailMessageId=${gmailMessageId}`);
  }

  return found.value;
}

/**
 * Optional simple auth:
 * - Authorization: Bearer <secret>
 * or payload.auth_token
 */
function authorizeRequest_(e) {
  const secret = PropertiesService.getScriptProperties().getProperty('SHARED_SECRET');
  if (!secret) return;

  const headers = e && e.headers ? e.headers : {};
  const authHeader =
    headers.Authorization ||
    headers.authorization ||
    headers.AUTHORIZATION ||
    null;

  let provided = null;
  if (authHeader && authHeader.startsWith('Bearer ')) {
    provided = authHeader.slice('Bearer '.length).trim();
  } else {
    try {
      const payload = parseJsonBody_(e);
      provided = payload.auth_token || null;
    } catch (_) {
      provided = null;
    }
  }

  if (!provided || provided !== secret) {
    throw new Error('Unauthorized');
  }
}

/**
 * Script Properties persistence
 */
function buildTaskKey_(payload) {
  return `${payload.project}:${payload.task_id}`;
}

function loadTaskMeta_(taskKey) {
  const raw = PropertiesService.getScriptProperties().getProperty(
    CONFIG.PROPERTY_PREFIX + taskKey
  );
  return raw ? JSON.parse(raw) : null;
}

function saveTaskMeta_(taskKey, meta) {
  PropertiesService.getScriptProperties().setProperty(
    CONFIG.PROPERTY_PREFIX + taskKey,
    JSON.stringify(meta)
  );
}

function deleteTaskMeta_(taskKey) {
  PropertiesService.getScriptProperties().deleteProperty(
    CONFIG.PROPERTY_PREFIX + taskKey
  );
}

/**
 * Labels
 */
function getOrCreateLabel_(name) {
  const existing = GmailApp.getUserLabelByName(name);
  return existing || GmailApp.createLabel(name);
}

function safeRemoveLabel_(thread, label) {
  try {
    thread.removeLabel(label);
  } catch (_) {}
}

/**
 * Utilities
 */
function parseJsonBody_(e) {
  if (!e || !e.postData || !e.postData.contents) {
    throw new Error('Missing JSON request body');
  }
  return JSON.parse(e.postData.contents);
}

function getRequiredScriptProperty_(key) {
  const value = PropertiesService.getScriptProperties().getProperty(key);
  if (!value) {
    throw new Error(`Missing required script property: ${key}`);
  }
  return value;
}

function jsonResponse(obj, statusCode) {
  const out = ContentService.createTextOutput(JSON.stringify(obj, null, 2));
  out.setMimeType(ContentService.MimeType.JSON);
  return out;
}
```

## Important correction to the prior design

One key change is worth making explicit: for reliable Gmail threading, the **subject should stay stable across the whole task**. Google’s thread requirements say the `Subject` must match when adding a message to a thread, alongside `threadId` and reply headers. [stackoverflow](https://stackoverflow.com/questions/51786588/reply-to-an-email-in-gmail-with-appscript-with-changed-recipients-ends-up-in-a-n)

So instead of:

- `[project][source][waiting][task:123] Need approval`
- later `[project][source][start][task:123] Resuming work`

you should use a stable canonical subject like:

- `[project][source][task:123] Need approval`

and put the changing state (`waiting`, `start`, `completed`) in the **body** and labels, not the subject. That is the most important design adjustment for your “same thread + auto-dismiss” requirement. [developers.google](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages)

## Recommended follow-up improvements

A few practical upgrades your coding agent should probably add next:

- Track both the **root** RFC Message-ID and the **last** RFC Message-ID; for replies, using the last message as `In-Reply-To` is a sensible default. The Gmail API lets you fetch message headers after sending. [labnol](https://www.labnol.org/detect-gmail-replies-251209)
- Add an endpoint like `GET ?task_id=...&project=...` for debugging thread metadata.
- Add a `dedupe_key` or `event_id` to suppress duplicate notifications from flaky callers. That is design advice from me; I do not have a primary-source doc here for your exact dedupe scheme.
- Optionally migrate metadata from Script Properties to a Google Sheet once volume grows, because Script Properties are simple but not ideal for richer debugging. That is also my analysis rather than a primary-source claim.

## Setup notes for your coding agent

Your coding agent should also be told to do these steps:

1. Enable **Advanced Gmail Service** in the Apps Script editor. [developers.google](https://developers.google.com/apps-script/reference/gmail)
2. Ensure the Gmail API is enabled for the backing Google Cloud project if Apps Script prompts for it. [mailtrap](https://mailtrap.io/blog/send-emails-with-gmail-api/)
3. Set Script Properties:
   - `TARGET_EMAIL`
   - `SHARED_SECRET` optionally
4. Deploy as a Web App and POST JSON events to the deployment URL.
