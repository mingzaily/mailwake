# HTTP API implementation

[Contributor index](README.md) · [Public HTTP API reference](https://mailwake.oritx.com/en/docs/api/)

The documentation site owns endpoint tables, request examples and script usage. This reference describes implementation boundaries and invariants for contributors.

## Ownership and authentication

`internal/httpapi/` owns routing, input decoding, status codes and safe error responses. `internal/auth/` owns setup, password hashes, sessions and API tokens. `internal/settings/` owns validated configuration and revisions; `internal/storage/` owns transactional persistence. App routes declare their required scope in the `appRoutes` table; see [App management](../internal/appmanagement/README.md).

Before setup, protected management endpoints return `setup_required`. Browser sessions use HttpOnly cookies and CSRF on mutations. Script APIs use Bearer tokens. App controller credentials have separate device and scope boundaries. Validate at the owning boundary and pass resolved authorization onward.

Configuration mutations use revision optimistic concurrency. Preserve write-only credentials when omitted, return a conflict for stale revisions, and keep user input available for correction. Test observable status codes and persisted state, including unauthorized access, scope isolation, stale revisions, invalid values and atomic updates.

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
- Standard folder roles and common folder names are localized in the console. Raw IMAP paths, custom names and `folder_roles` remain unchanged in APIs; Apps localize these roles with their own catalogs. Notification titles continue to use the original leaf folder name. User-entered names, email subjects, message bodies and verification codes remain literal.
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

Each mailbox reserves one connection for its folder discovery and connection tests. Watcher demand is `realtime Folder count + (1 if any scheduled Folders exist, otherwise 0)`, at most `connection_limit - 1` connections (9 by default); all operations for that mailbox together stay within `connection_limit` (2–100). Saved configurations with a limit of 1 start in recovery mode with `configuration_invalid`.

## Native target and content contracts

The native notification target is one active pairing. Revoking the selected target clears the channel and target in the same transaction and increments the configuration revision; other channels and targets retain their values. Preserve existing delivery plans and history according to [pairing revocation synchronization](native-push.md#pairing-revocation-synchronization).

Message content requires a valid controller credential, explicit content scope, a signed message reference and a currently subscribed folder. Keep body and image payloads within the limits in [HTML content](html-content.md) and outside persistent history and diagnostics.

When changing API behavior, update the public API article in both supported content languages and the corresponding tests in `internal/httpapi/`. Keep its error codes, privacy semantics and response shapes aligned with this implementation.
