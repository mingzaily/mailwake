# HTTP API implementation

[Contributor index](README.md) · [Public HTTP API reference](https://mailwake.oritx.com/en/docs/api/)

The documentation site owns endpoint tables, request examples and script usage. This reference describes implementation boundaries and invariants for contributors.

## Ownership and authentication

`internal/httpapi/` owns routing, input decoding, status codes and safe error responses. `internal/auth/` owns setup, password hashes, sessions and API tokens. `internal/settings/` owns validated configuration and revisions; `internal/storage/` owns transactional persistence. App routes declare their required scope alongside registration through `appRouteRegistrar`; see [App management](../internal/appmanagement/README.md).

Before setup, protected management endpoints return `setup_required`. Browser sessions use HttpOnly cookies and CSRF on mutations. Script APIs use Bearer tokens. App controller credentials have separate device and scope boundaries. Validate at the owning boundary and pass resolved authorization onward.

`POST /api/v1/session` returns 401 `login_credentials_invalid` for an incorrect username or password, with the same message for either field. Protected requests with missing or invalid session/API credentials return 401 `unauthorized`. Failed logins retain the shared failure limit and issue no session cookie.

Configuration mutations use revision optimistic concurrency. Preserve omitted write-only credentials only for the same target (see below), return a conflict for stale revisions, and keep user input available for correction. Test observable status codes and persisted state, including unauthorized access, scope isolation, stale revisions, invalid values and atomic updates.

## Service composition and response boundaries

`cmd/mailwake/` calls `internal/application` to assemble the local identity, native delivery, App management and mailbox runtime services. HTTP receives the App and native services explicitly. The runtime owns mailbox and delivery worker lifetimes and installs its transactional pairing-revocation callback before workers start. App invitation handling stays in `internal/appmanagement`.

`internal/coreidentity` owns the persisted Core signing identity, P-256 signatures and signed message references. App invitations and content verification use this local capability directly. Native delivery uses the same identity for Relay requests and notification references. The encrypted `native.identity` configuration key, vault AAD and signature formats remain stable across restarts and upgrades.

Mailbox, delivery and diagnostic handlers depend on the capabilities they use. `internal/settings/views.go` defines typed, allowlisted public views; credential values remain write-only. Web and App share business operations and error mapping. Web mailbox probes return folder discovery; App mailbox probes return only `{ "status": "connected" }`, preserving the separate folder scope.

Each protected App registration specifies its method, path, scope and handler together. Authentication resolves the controller once, enforces that scope, and passes the controller explicitly to self-revocation and saved-test limiting. Unknown routes use the standard 404 response. Management authorization and push pairing retain independent lifecycles; selected-target revocation keeps its configuration revision and transaction boundaries.

## Languages and error codes

English is the default. The API, Web console and notification renderers share the embedded catalogs in `internal/i18n/locales/`. `internal/i18n/languages.json` defines these canonical codes and native names:

| Code | Language |
| --- | --- |
| `en` | English |
| `zh-CN` | 简体中文 |
| `zh-Hant` | 繁體中文 |
| `ja` | 日本語 |
| `ko` | 한국어 |
| `de` | Deutsch |
| `fr` | Français |
| `es` | Español |
| `pt-BR` | Português (Brasil) |

- On first visit, the console uses the browser’s `Accept-Language`. Its selector stores the chosen canonical code locally. Switching the console language leaves saved notification settings unchanged.
- API requests, including App management requests, negotiate `Accept-Language` by quality weights. Responses include `Content-Language` and `Vary: Accept-Language`. Unsupported preferences are skipped; missing, invalid or entirely unsupported preferences fall back to `en`. A language with weight 0 is excluded from negotiation.
- Regional variants map to their supported language: `fr-CA` → `fr`, `es-MX` → `es`, `pt` / `pt-PT` → `pt-BR`. Chinese script is respected: `zh-Hans` / `zh-CN` / `zh-SG` → `zh-CN`; `zh-Hant` / `zh-TW` / `zh-HK` / `zh-MO` → `zh-Hant`. An explicit script takes precedence over region; plain `zh` uses Simplified Chinese.
- `GET /locales/default.json` serves the negotiated catalog; `GET /locales/{canonical-code}.json` serves that catalog explicitly. Unknown catalog paths return `locale_not_supported`.
- Delivery settings `language` controls Core-rendered notifications independently of the request or console language. It accepts the canonical codes and supported regional variants, persists the canonical code, and defaults to `en` when omitted or empty. An unsupported explicit value returns 400 `config_language_invalid`.
- Relay generates Native test notification text using the paired device’s language. The Core notification language does not override that language. Relay and App language support is implemented in their respective modules.
- Standard folder roles and common folder names are localized in the console. Raw IMAP paths and `folder_roles` remain unchanged in APIs; discovery also returns optional `folder_names`, a path-to-leaf map derived from each LIST response delimiter. NIL delimiters and empty leaf names retain the original path. Web owner mailbox responses include optional `folder_display` with the saved `folder_names` and `folder_roles`; App mailbox responses omit that directory to preserve the folders scope boundary. Display refresh keeps the configuration revision and returns `settings_conflict` (409) if the mailbox changes during scanning. Apps localize these roles with their own catalogs. Notification titles continue to use the original leaf folder name. User-entered names, email subjects, message bodies and verification codes remain literal.
- Logs and CLI output use English. Business errors retain stable English `snake_case` codes and named parameters; storage keeps codes and parameters, and display layers translate messages. Dates and numbers in API responses keep their existing machine-readable formats; the console formats timestamps using its chosen locale.

```json
{
  "error": {
    "code": "unauthorized",
    "message": "Sign in with a valid session or API token."
  }
}
```

`last_error` in delivery records and `notice` in folder status use the same `{code, message, params?}` shape, for example `bark_http_error` with `{"status":"503"}`. Branch on `code`; show `message`.

`connections_in_use` reports currently occupied connection slots, including transient management operations. Scheduled checks release their connection between rounds. The console computes configured monitoring demand from folder check modes; this differs from the instantaneous API count.

Each mailbox reserves one connection for its folder discovery and connection tests. Watcher demand is `realtime Folder count + (1 if any scheduled Folders exist, otherwise 0)`, at most `connection_limit - 1` connections (9 by default); all operations for that mailbox together stay within `connection_limit` (2–100). Saved configurations with a limit of 1 start in recovery mode with `configuration_invalid`.

## Native target and content contracts

The native notification target is one active pairing. Revoking the selected target clears the channel and target in the same transaction and increments the configuration revision; other channels and targets retain their values. Preserve existing delivery plans and history according to [pairing revocation synchronization](native-push.md#pairing-revocation-synchronization).

Message content requires a valid controller credential, explicit content scope, a signed message reference and a currently subscribed folder. Keep body and image payloads within the limits in [HTML content](html-content.md) and outside persistent history and diagnostics.

When changing API behavior, update the public API article in all nine published languages and the corresponding tests in `internal/httpapi/`. Keep its error codes, privacy semantics and response shapes aligned with this implementation.

## Credential target binding

Mailbox tests and saves require an explicit valid password when `host`, `port` or `username` changes and a password is already saved. Otherwise they return HTTP 400 `mailbox_password_required_for_change` before opening a connection. Changes to labels or connection limits retain the password when those three fields stay equal.

Bark tests and saves require an explicit valid device key when the effective `bark.endpoint` changes and a key is already saved. Otherwise they return HTTP 400 `bark_key_required_for_change` before sending. This also applies while another channel is selected, preserving the binding when Bark is enabled later. Omitting the endpoint preserves its saved value; first-time configuration defaults to `https://api.day.app`. Endpoint comparison includes the entire configured address, including port, path and trailing slash. Re-enter the key for any address change.

These checks share the settings merge boundary for Web and App requests. Omitted or null credentials reuse saved values only for the same target; empty, whitespace-only or newline-containing replacement values are rejected. A rejected change preserves the stored configuration, revision and active runtime. Tests using explicitly supplied replacement credentials leave configuration unchanged. Core decrypts credentials to connect to the selected service; these rules restrict management operations, while server operators with the encryption key can still read credentials.

### Draft mailbox discovery and initial subscriptions

`POST /api/v1/app/mailboxes/scan` accepts the same credential draft as `mailboxes/test` and returns `FolderDiscovery` (`folders`, optional `folder_names` and `folder_roles`). It requires both `mailboxes` and `folders` scopes. The probe authenticates and lists selectable folders without creating configuration, subscriptions or watchers. The existing App test routes continue returning connection status only.

`POST /api/v1/mailboxes` and its App counterpart accept an optional `folders` array of `{name,check}` alongside the mailbox fields. Creation normalizes subscriptions, checks the connection budget and verifies every selected folder against the fresh server directory. Credentials and initial subscriptions commit in one transaction before watchers start; both revisions begin at 1. Omitting folders creates an empty subscription list. App requests with a nonempty selection require both management scopes; mailbox-only permission continues to allow creation without subscriptions. A failed save leaves both records absent. Clients keep drafts until final confirmation and use the existing duplicate-mailbox identity rule on retries.
