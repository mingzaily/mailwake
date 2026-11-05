# App mail history and monitoring health

[API reference](api.md) · [Monitoring](monitoring.md) · [Message content](html-content.md)

This version 1 contract adds direct App-to-Core reads. All endpoints require a current device-bound `mwac_` bearer credential and return `Cache-Control: no-store`. Platform and Relay do not participate. Times are UTC RFC3339 strings and may include fractional seconds. Identifiers, folder paths and cursors are case-sensitive opaque values.

[Synthetic consumer fixtures](fixtures/app-mail-v1.json) cover capability permission, the catalog, an empty continuing page, a completed message page, all five health states and stable HTTP errors. Each named entry is an independent mock response. Their signatures and cursors are placeholders for client tests; a real Core issues and validates its own signed values.

## Authorization and discovery

`GET /api/v1/app/capabilities` accepts every valid App controller, including one without `content`. Its response describes supported protocols and the current controller's reading permission.

```json
{
  "core_id": "example-core",
  "mail_history": { "version": 1, "window_days": 30, "max_page_size": 100 },
  "mail_health": { "version": 1 },
  "permissions": { "content": true }
}
```

The three `/mail/` endpoints require `content`. This is the same free owner-approved permission as `/content` and `/content/read`. Current Core controllers grant scopes across the instance. A controller with `content` can read all currently subscribed folders of that Core. Core has no per-mailbox controller grants. Management and diagnostics scopes grant their own operations. Native push pairing alone grants no mail-reading credential.

Capability version 1 supports every field and state below. An older Core may return `404 route_not_found` for discovery. Clients retain existing notification and content behavior and present the history/health feature as requiring a capable Core.

## Subscribed folder catalog

`GET /api/v1/app/mail/folders?limit=50` returns the local subscription catalog without opening an IMAP connection. `limit` defaults to 50 and accepts 1–100. Pass the returned `cursor` query parameter for subsequent pages.

```json
{
  "core_id": "example-core",
  "items": [{
    "mailbox_id": "example-mailbox",
    "account_id": "example-account",
    "mailbox_label": "Personal",
    "folder": "INBOX",
    "folder_name": "Inbox",
    "folder_role": "inbox",
    "subscription_revision": 3
  }],
  "complete": true
}
```

`folder_name` and `folder_role` are optional server-derived presentation values. `folder` is the exact subscribed IMAP path. The catalog includes subscribed resources only, ordered by mailbox ID then folder path. It exposes no server address, login name, password or management configuration. UIDVALIDITY comes from message pages, since reading the catalog does not contact the provider.

`items` is always an array. `next_cursor` is present only while more catalog items remain. Catalog cursors expire after 15 minutes and bind the controller, device, endpoint and catalog identity. A mailbox identity or subscription revision change invalidates continuation with `409 sync_cursor_invalid`. Restart catalog pagination and replace the cached resource set only after collecting a complete catalog. Removing a resource invalidates its local sync generation and sensitive content according to the App's cache lifecycle.

Catalog and health pages share the 256 KiB encoded JSON response budget with message pages. A page can contain fewer items than the requested limit; its cursor continues at the first omitted resource. Clients use `complete` and `next_cursor` to determine completion.

## Recent message page

`POST /api/v1/app/mail/messages` starts a folder scan with the identity from the catalog.

```json
{
  "mailbox_id": "example-mailbox",
  "account_id": "example-account",
  "folder": "INBOX",
  "limit": 50
}
```

Subsequent requests send the same three resource fields plus `cursor`. `limit` may vary within 1–100. The explicit `account_id` prevents a first page started from a stale catalog from reading a replacement account.

```json
{
  "core_id": "example-core",
  "mailbox_id": "example-mailbox",
  "account_id": "example-account",
  "folder": "INBOX",
  "subscription_revision": 3,
  "window_start": "2026-10-06T12:00:00Z",
  "window_end": "2026-11-05T12:00:00Z",
  "uid_validity": 42,
  "snapshot_max_uid": 9000,
  "items": [{
    "message_ref": {
      "core_id": "example-core",
      "mailbox_id": "example-mailbox",
      "account_id": "example-account",
      "folder": "INBOX",
      "uid_validity": 42,
      "uid": 8990,
      "signature": "opaque-message-signature"
    },
    "sender": "alerts@example.test",
    "sender_name": "Example alerts",
    "subject": "A sample message",
    "received_at": "2026-11-04T09:00:00Z",
    "is_read": false
  }],
  "next_cursor": "opaque-page-cursor",
  "complete": false
}
```

Core fixes `T = window_end` at the first request, with the inclusive interval `[T - 30 × 24 hours, T]`. Filtering uses INTERNALDATE. The email Date header has no influence. Each round also fixes its upper UID bound at EXAMINE's UIDNEXT minus one. Newer UIDs enter the next round. `snapshot_max_uid` is zero for an empty initial UID space. Servers that omit UIDNEXT return `503 message_unavailable` because they cannot establish this bounded snapshot.

Pages scan descending finite UID ranges, at most 4096 UID positions per request. SEARCH uses a conservative date range followed by exact timestamp filtering. FETCH requests metadata for at most `limit` candidates. Imported messages can have old dates and large UIDs, so encountering an old message never ends the round. A page may contain zero `items` and a `next_cursor`. The client continues until `complete` is true. A complete scan covers this round's fixed range, while concurrent flag changes and deletions converge on the next refresh. Complete pages omit `next_cursor`.

Items arrive in descending UID order. Clients sort their collected set by `received_at` and then stable message identity and preserve their scroll anchor as later pages arrive. Identity is `(core_id, mailbox_id, account_id, folder, uid_validity, uid)` regardless of signature or notification event ID. History and pushes merge by that identity. Pass `message_ref` unchanged to the existing content and read-action endpoints.

`sender` and `subject` are always strings and may be empty. `sender_name` is optional. MIME names and subjects use the existing decoder. Output removes control and bidirectional formatting characters and caps sender addresses/names at 512 UTF-8 bytes and subjects at 1024 bytes. `is_read` reflects the fetched `\Seen` flag. Version 1 omits the optional `preview` field and performs no body reads. Clients accept an absent preview.

A page has a 20-second total deadline, a 2 MiB provider-response budget, at most 100 items and a 256 KiB encoded JSON response budget. The response budget can shorten a successful page and its cursor resumes before the first omitted candidate. Metadata exceeding the provider-response budget fails with `413 sync_page_too_large`. A lower page limit can reduce metadata transfer. EXAMINE and metadata FETCH preserve flags. The independent connection uses the mailbox's existing non-watcher connection budget and shared Outlook token provider. Active watchers retain their leases. Contending read requests fail promptly with `503 message_unavailable`.

Message cursors are signed, expire 24 hours after T, and bind Core, controller/device, resource identity, subscription revision, fixed window, UIDVALIDITY, UID upper bound and next scan position. Each request authenticates its current controller. Runtime rechecks the mailbox identity and subscription after the read to reject results from a concurrent configuration replacement. UIDVALIDITY, account or subscription changes and expired/invalid/cross-device cursors return `409 sync_cursor_invalid`. A removed mailbox or unsubscribed folder returns `404 message_not_found`. Retry either failure from a fresh catalog and fresh scan as appropriate. A failed page contributes no progress.

## Folder monitoring health

`GET /api/v1/app/mail/health?limit=50` uses the same catalog pagination and `content` permission. Its cursor is specific to this endpoint. Reads sample existing engine state and perform no IMAP probes.

```json
{
  "core_id": "example-core",
  "observed_at": "2026-11-05T12:00:00Z",
  "items": [{
    "mailbox_id": "example-mailbox",
    "account_id": "example-account",
    "mailbox_label": "Personal",
    "folder": "INBOX",
    "subscription_revision": 3,
    "state": "action_required",
    "reason_code": "outlook_reauthorize",
    "updated_at": "2026-11-05T11:58:00Z",
    "check": "realtime",
    "last_checked_at": "2026-11-05T11:57:00Z"
  }],
  "complete": true
}
```

| State | Meaning |
| --- | --- |
| `starting` | Subscribed monitor has started and awaits its first successful check. |
| `healthy` | Last check succeeded, including scheduled monitors waiting for their next normal check. |
| `retrying` | A check failed and Core will retry automatically. |
| `action_required` | Provider refused credentials. Core spaces authentication attempts by 30 minutes, and the user should update credentials or reauthorize Outlook. |
| `paused` | Monitoring is stopped because the saved configuration is invalid or its subscribed workload exceeds the connection budget. |

`updated_at` records the last engine status update, with `observed_at` recording the read time. `last_checked_at` is optional and records the last successful check. `check` is `realtime`, `5m` or `15m`. Every returned state belongs to one subscribed folder and the account identity shown. Group by mailbox and retain folder counts when presenting partial failures. An affected folder cannot imply that healthy folders have stopped.

`reason_code` is absent for healthy/starting states. Authentication failures use `mail_auth_failed` or `outlook_reauthorize`. Retry reasons include `imap_connection_failed`, `imap_connection_closed`, `imap_command_timeout`, `imap_login_failed`, `imap_capability_failed`, `imap_folder_open_failed`, `imap_folder_rejected`, `imap_status_failed`, `imap_metadata_failed`, `imap_idle_start_failed`, `outlook_unavailable`, `connection_budget_exceeded`, `checkpoint_storage_failed` and `mail_check_failed`. Paused reasons use `configuration_invalid` or `connection_budget_exceeded`. Raw errors and provider explanations are omitted. Successful checks clear the prior reason immediately. A failed HTTP read is a failure to obtain health information, not a newly reported mailbox fault.

Catalog membership is live. Changed/deleted resources disappear from later reads and invalidate continuation cursors. Finish a complete health catalog before removing cached resources solely because of absence. Pages report their own observation times and do not claim a transactionally frozen health snapshot.

## Errors and caching

Existing error JSON remains `{ "error": { "code": "...", "message": "..." } }` with localized messages. Clients branch on codes.

| HTTP | Code | Client action |
| --- | --- | --- |
| 400 | `request_invalid` | Correct fields or limit. |
| 401 | `unauthorized` | Restore or obtain current device authorization. |
| 403 | `content_permission_required` | Obtain owner-approved content permission. |
| 404 | `message_not_found` | Refresh catalog and invalidate the removed resource. |
| 409 | `sync_cursor_invalid` | Start a fresh scan, retaining readable cache during refresh. |
| 413 | `sync_page_too_large` | Retry the same cursor with a smaller limit. At limit 1, retain committed pages and report the folder as incomplete; provider responses can exceed the read budget even for one message. |
| 503 | `message_unavailable` | Retry later. |
| 504 | `message_timeout` | Retry the same page when connectivity permits. |

Request cancellation closes the read connection and releases its budget. History reads do not write checkpoints, outbox, notification history or server flags. No email metadata, signatures, cursors or response bodies enter logs or diagnostic exports. HTTP no-store leaves App-owned encrypted/local cache behavior to the client. Clients commit each page and its cursor together, preserve readable data during refresh failure, and discard responses from obsolete resource generations.

## Verification and operational limits

`go test -tags nomsgpack,mailwake_test ./internal/httpapi -run TestMailHistoryAcrossHTTPRuntimeAndIMAP -v` exercises real device authorization, the runtime, SQLite and a local TLS IMAP server with 300 synthetic imported messages. It verifies complete pagination, unchanged checkpoints/outbox, mailbox read flags, usable content references, healthy scheduled monitoring, diagnostic privacy and UIDVALIDITY invalidation. Focused tests in `internal/mail/imap`, `internal/runtime`, `internal/mail` and `internal/engine` cover exact time boundaries, sparse UID ranges, size/cancellation budgets, cursor binding, concurrent resource replacement and failure/recovery state.

Sparse or long-lived UID spaces can require many empty pages. The finite scan budget bounds each request rather than claiming a maximum round duration. Retain and resume the cursor; after its 24-hour expiry start a fresh round. Very large individual envelopes can exceed the provider budget even at a one-item limit. Report that folder as incomplete instead of skipping the message. Optional previews, server-side full-text search and historical windows beyond 30 days remain outside version 1.

Local fixtures establish protocol and lifecycle behavior. Release acceptance separately checks real Gmail, Outlook.com and the supported Microsoft 365 tenant configuration, measuring page latency and watcher reconnection under concurrent use. Final iOS acceptance covers durable page/cursor storage, push merging, obsolete-generation cancellation, cached reading and localized health presentation.
