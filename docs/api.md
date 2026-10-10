# HTTP API reference

[简体中文](api.zh-CN.md) · [Documentation](README.md)

## Languages and error codes

English is the default. The only message catalogs are `internal/i18n/locales/en.json` and `zh-CN.json`, shared by the API, the management interface and notifications, and embedded in the binary.

- The page picks its language from `Accept-Language` and can switch between English and Simplified Chinese; only the language preference is stored in the browser, never the token.
- The API negotiates the language from `Accept-Language` weights, returns `Content-Language` and `Vary: Accept-Language`, and falls back to `en`.
- `language` in delivery settings (`en` or `zh-CN`) sets the notification language.
- Logs and CLI output are in English. Business errors use stable English `snake_case` codes with named parameters; storage keeps codes and parameters, and messages are translated when displayed.

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Sign in with a valid session or API token."
  }
}
```

`last_error` in delivery records and `notice` in folder status use the same `{code, message, params?}` shape, for example `bark_http_error` with `{"status":"503"}`. Branch on `code`; show `message`.

Each mailbox reserves one connection for its folder discovery and connection tests. Watcher demand is `realtime Folder count + (1 if any scheduled Folders exist, otherwise 0)`, at most `connection_limit - 1` connections (9 by default); all operations for that mailbox together stay within `connection_limit` (2–100). Saved configurations with a limit of 1 start in recovery mode with `configuration_invalid`.
## HTTP API

`GET /` serves the current page; static assets and `/healthz` are public. Before setup, every other management endpoint returns 403 `setup_required`. `POST /api/v1/setup` is available once; `POST /api/v1/session` is public after setup. All remaining APIs require the session cookie or `Authorization: Bearer mwk_…`.

Cookie sessions use HttpOnly, SameSite=Strict, Path=/, Secure on a TLS connection or an HTTPS Origin, a seven-day idle timeout and a 30-day absolute timeout. Every cookie-authenticated mutation requires `X-CSRF-Token` and an HTTP(S) `Origin` whose host and optional port exactly match the request Host. Bearer requests skip CSRF checks. An invalid Authorization header fails even when a valid cookie is present.

Login and setup code failures share a source-address budget: ten failures per 15 minutes, followed by 429 `too_many_attempts` with `Retry-After` in seconds. The source is Gin ClientIP using the trusted proxy ranges above. Password changes also use this budget. Explicit cross-site Origins are rejected on login and setup. Because every private-network peer is trusted as a proxy, a client on your local network can set X-Forwarded-For and spread its attempts across many source addresses. Do not expose the Core listener to an untrusted local network; publish it through your reverse proxy only.

All JSON bodies are limited to 64 KiB; unknown fields or trailing JSON return 400. Passwords are 12 characters minimum and 1024 UTF-8 bytes maximum. Usernames and token names are 1–128 bytes. All management API timestamps are RFC 3339 strings in UTC, including delivery `created_at`/`accepted_at`, token `created_at`/`last_used_at`, folder checks, log times and diagnostic generation time. Fractional seconds preserve stored precision. Unused token `last_used_at` is null; deliveries without acceptance omit `accepted_at`. Counts, durations and revisions remain numbers.

401 identifies an invalid session or API token, or rejected credentials on `POST /api/v1/session`. An incorrect setup code returns 400 `setup_code_invalid`; the wizard returns to the code step and preserves the username. An incorrect current password on `PUT /api/v1/admin/password` returns 400 `current_password_invalid` and counts toward the failure limit; the current session remains valid. Login failures and current-password errors appear within the form, preserving its inputs.

| Method | Path | Request / response |
| --- | --- | --- |
| POST | `/api/v1/setup` | `{ "code":"XXXX-XXXX-XXXX", "username":"admin", "password":"…" }`; 201, cookie + `{ "csrf_token":"…" }`; repeat setup: 409 |
| POST | `/api/v1/session` | `{ "username":"admin", "password":"…" }`; 200, cookie + CSRF token |
| GET | `/api/v1/session` | `{ "authenticated":true, "csrf_token":"…" }`; Bearer returns `method: "token"` instead of CSRF |
| DELETE | `/api/v1/session` | 204; revokes the current cookie session; requires a session |
| PUT | `/api/v1/admin/password` | `{ "current_password":"…", "password":"…" }`; 204, revokes other sessions; Bearer revokes all sessions |
| POST | `/api/v1/tokens` | `{ "name":"automation" }`; 201, `{ "id":"…", "name":"…", "created_at":"2026-09-29T01:02:03Z", "last_used_at":null, "token":"mwk_…" }` |
| GET | `/api/v1/tokens` | `{ "tokens":[…] }`; metadata only |
| DELETE | `/api/v1/tokens/:id` | 204; revokes a token; repeat deletion also returns 204 |
| GET | `/api/v1/mailboxes` | `{ "mailboxes":[…] }`; redacted mailbox views |
| POST | `/api/v1/mailboxes` | Create with the mailbox form below; 201, redacted view with generated `id` and revision 1 |
| GET / PUT / DELETE | `/api/v1/mailboxes/:id` | Read / update with current revision / delete (204); missing ID returns 404 `mailbox_not_found` |
| POST | `/api/v1/mailboxes/test` | Test an unsaved form; returns `{ "folders":[…] }`, keeps configuration unchanged |
| POST | `/api/v1/mailboxes/:id/test` | Test the form with omitted credentials merged from this mailbox |
| GET | `/api/v1/mailboxes/:id/folders` | Discover selectable folders using this mailbox’s budget |
| GET / PUT | `/api/v1/mailboxes/:id/subscriptions` | Read / save subscriptions using their own revision; successful save returns 202 |
| GET / PUT | `/api/v1/settings/delivery` | Read / save global delivery settings with their own revision |
| DELETE | `/api/v1/settings/delivery` | Clear saved channel and credentials with `{revision}`; 204, stale revision 409 |
| POST | `/api/v1/settings/delivery/test` | Test the delivery form with saved credentials merged; 204, configuration unchanged |
| GET | `/api/v1/status` | Folder states, channel, delivery counts and scoped notices |
| GET | `/api/v1/diagnostics` | Download a redacted JSON report without runtime logs |
| GET | `/api/v1/logs` | Authenticated operational logs; `after`, `level`, `mailbox_id`, `limit` filters (see below) |
| GET | `/api/v1/deliveries` | Last 50 delivery records, including subject and mailbox/folder summary |
| POST | `/api/v1/test-push` | Queue a test notification; 202 |
| POST | `/api/v1/deliveries/:id/retry` | Requeue a `dead` task; 202 |

Delivery records include an optional `message` object: `mailbox_id`, `mailbox_label`, `folder`, `subject` (string or null) and `test` (boolean). Names are snapshots from enqueue time and survive mailbox renaming or deletion. A null subject means it was not retained; an empty string means the mail had no subject. Test notifications have `test=true` and no mailbox source. The API omits sender and body from this summary.


Mailbox creation example:

```json
{"label":"Work mailbox","host":"imap.example.org","port":993,"username":"user@example.org","password":"…","connection_limit":10}
```

Core allows at most 20 saved mailboxes, including invalid configurations awaiting repair; the next creation returns 400 `mailbox_limit_exceeded`. IDs are generated as `mbx_` plus a random string and stay stable. `label` defaults to the username (email address), allows at most 64 Unicode characters and rejects line breaks. A username longer than 64 characters needs an explicit shorter label. Omitted labels on update keep the saved value; an empty label uses the submitted username.

Mailbox PUT uses the same form plus `"revision":1` (the value read from GET), and requires host, port and username. Omitted password keeps the saved password; an explicit empty password is rejected. Omitted `connection_limit` keeps the current value; new mailboxes default to 10, range 2–100. POST creation omits revision or sends 0. Test routes accept the same fields without requiring a revision and keep the configuration unchanged. The unsaved test requires credentials; the saved-mailbox test merges omitted credentials only from the specified ID.

A mailbox GET view includes `id`, `label`, `revision`, `host`, `port`, `username`, `connection_limit`, `connections_in_use` and `"password":{"configured":true}`. A recovered invalid mailbox reports `configured:false` and keeps its ID and revision so it can be repaired. The GET password object is a view, not a valid write value.

Each mailbox has an independent connection budget. Realtime Folders each consume one slot; all its scheduled Folders together consume one. Their combined demand must fit within `connection_limit - 1`, reserving one slot for management. Subscription PUT and mailbox limit changes enforce this formula; exhausted capacity returns `connection_budget_exceeded`. Testing an unsaved mailbox uses a temporary budget. The unsaved test, saved-mailbox test and folder discovery endpoints additionally share a global concurrency limit of 2; excess requests return 429 `discovery_busy` immediately. Completion or cancellation releases admission.

Delivery PUT example for an initially unconfigured channel:

```json
{"revision":0,"channel":"bark","preview":"off","retry_count":0,"language":"en","bark":{"endpoint":"https://api.day.app","key":"…"}}
```

Read `/settings/delivery` before each update and send its revision; the first save advances 0 to 1. Pushover uses `"pushover":{"token":"…","user":"…"}`; Webhook uses `"webhook":{"url":"https://hooks.example.org/mail","secret":"…"}`. Channel, preview and revision are required. Omitted language and retry count use defaults. Omitted credentials, including Webhook URL, keep saved values; an explicit empty active credential is rejected. GET replaces every credential with `{"configured":true|false}`. The separate test endpoint does not require revision.

Mailbox creates and connection changes test connection/login before saving. If host, port, username and connection_limit match the saved values and password is omitted, PUT changes only the label: revision checking and persistence still apply, while tests and watcher replacement are skipped. Queued tasks retain their original label; future tasks use the new label. Creating or updating an identity already used by another mailbox returns 409 `mailbox_duplicate` (host and username compare case-insensitively, port exactly); concurrent creation can save that identity only once. Delivery PUT tests only changes to the channel type or active channel address/credentials; preview, retry and language edits save directly. Tests run outside configuration locks and keep existing watchers active. A failed test preserves the previous configuration. Stale settings revisions return 409 `settings_conflict`; a subscription change during a mailbox test also invalidates that test’s snapshot. Reload before retrying. Each mailbox and the global delivery settings have independent revisions, so edits to different mailboxes or to a mailbox and delivery do not conflict.

A successful PUT returns the redacted view and applies immediately. A mailbox connection change stops its own watchers before the save and optional cursor reset; a failed save restores them. Other mailboxes continue running. Delivery changes preserve the snapshot of an in-flight attempt; subsequent attempts use the new one. Pending tasks wait while delivery is unconfigured.

`GET /api/v1/mailboxes/:id/subscriptions` returns `{"revision":1,"folders":[{"name":"INBOX","check":"realtime"},{"name":"Clients","check":"5m"}]}`. PUT sends the revision read and the complete `folders` array; an empty array unsubscribes from everything. Names are unique within each mailbox, INBOX is normalized, and each name is at most 4096 UTF-8 bytes. Every Folder requires `name` and `check`; check is `realtime`, `5m` or `15m`. Missing/invalid checks and the previous string-array format return 400. The connection-demand formula above replaces the Folder count limit. Changing only check preserves progress, catches up new mail and keeps existing event IDs; cancelling and later subscribing again builds a fresh baseline. All fields are required. Saving returns 202 with the next revision; stale revisions return 409 `subscriptions_conflict`. Folder existence is confirmed by the watcher, so subscriptions can be removed while the mailbox is offline. Storage failures return 503 and preserve current subscriptions.

Every `/status.folders[]` item includes `mailbox_id`, `mailbox_label` and `check`. `mode` is `idle`, `poll` or `scheduled`; scheduled Folders also include `next_check` (RFC 3339 time, including retry times). Realtime Folders omit `next_check`. Saved configuration that fails JSON or model validation becomes unconfigured, with `configuration_invalid` in `/status.notices` under `mailbox:<id>` or `delivery`; PUT repairs that item. Excess subscriptions pause only that mailbox and report `connection_budget_exceeded` under `subscriptions:<id>`. Reducing subscriptions or increasing its limit resumes monitoring. Decryption failures stop startup.

The diagnostics allowlist contains generation time, build/Go/platform information, queue counts, and anonymous mailbox and watcher states, including `check` and `mode`. `mailboxes[].index` and `watches[].mailbox_index` identify “Mailbox 1”, “Mailbox 2”, and so on within that report; each mailbox’s watchers have their own `index`. Mailbox entries include configuration/subscription notice codes. Only `imap_folder_rejected` can include `watches[].response_code`. The report omits mailbox IDs, display names, addresses, server names, folder names, message data, task IDs, credentials and all other error parameters.

Task states: `pending` waits for a request, `accepted` means the channel accepted the request, `dead` means the attempt ended with a recorded failure.
## Runtime logs

Use `docker compose logs -f core` (or `docker logs <container>`) for JSON stderr output. Authenticated clients can query the same operational events through `GET /api/v1/logs?after=0&level=info&mailbox_id=mbx_example&limit=200`. A session cookie or API token is required.

The in-memory ring keeps the newest **2000** Info/Warn/Error entries and clears on restart. `after` is an exclusive non-negative sequence; `level` selects that level and above (`info` default); omitted `mailbox_id` includes all mailboxes and global operations. `limit` defaults to 200, range 1–500. Invalid parameters return 400 `logs_request_invalid`.

```json
{"entries":[{"seq":42,"time":"2026-09-29T00:00:00Z","level":"info","message":"Folder connection established","attrs":{"mailbox_id":"mbx_example","folder":"INBOX","mode":"idle","check":"realtime"}}],"next":42}
```

Use `next` as the next request’s `after`. At the page limit it is the last returned sequence; after scanning the snapshot it advances to the snapshot tail, including when filters match nothing. Evicted entries are skipped. If restart returns `next < after`, reset the cursor to 0. Entries are ordered by sequence.

Logs cover mailbox and Folder lifecycles, baseline/enqueue counts, delivery attempts, configuration changes and administrator actions. Global events use an empty `mailbox_id`. Only these scalar attributes are exposed: `mailbox_id`, `folder`, `mode`, `check`, `code`, `event_id`, `channel`, `attempt`, `duration_ms`, `http_status`, `next_attempt`, `backoff_seconds`, `count`, `revision`, `token_id`, `version`, `listen`. `http_status=0` means no HTTP response was received; `token_id` is a public record identifier, not the token value. Passwords, token/session values, setup codes, sender, subject, bodies and credential URLs are omitted. The setup-code line goes only to the console. Logs contain Folder names; the anonymous diagnostic export excludes logs and names.


Folder discovery and administrator mailbox tests return `folders` as the original path array plus optional `folder_roles`, a map from path to `inbox`, `drafts`, `sent`, `trash`, `junk`, `archive`, `all` or `flagged`. Roles come from IMAP SPECIAL-USE attributes; INBOX is recognized by its protocol-defined name. The Web UI falls back to case-insensitive exact matches for common English folder names when roles are absent. Translated names show the original path in parentheses; other names remain unchanged. Display translations never change subscription paths. Notification titles use the final segment of slash-separated folder paths; structured folder fields retain the complete path. App mailbox tests continue returning only connection status.

`GET /api/v1/session` includes `username` for an authenticated cookie session. Token authentication keeps its existing response.
