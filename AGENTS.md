# Mailwake Core contributor entry

## Read by task

Start with [README.md](README.md) and [CONTRIBUTING.md](CONTRIBUTING.md). This repository contains the self-hosted Core only. User tutorials live at [Mailwake documentation](https://mailwake.oritx.com/en/docs/); `docs/` contains English contributor references.

| Task | References | Owning code |
| --- | --- | --- |
| Builds, tests, fixtures or releases | [Development](docs/development.md), [Contributing](CONTRIBUTING.md) | `Makefile`, `.github/workflows/`, `web/scripts/` |
| Mailbox connections and folder watchers | [Monitoring](docs/monitoring.md) | `internal/mail/`, `internal/engine/`, `internal/runtime/` |
| Notification channels and retries | [Delivery](docs/notifications.md), [Storage](docs/storage.md) | `internal/delivery/`, `internal/storage/` |
| HTTP, sessions, tokens or localization | [API implementation](docs/api.md) | `internal/httpapi/`, `internal/auth/`, `internal/i18n/` |
| Console views or components | [Console](docs/console.md), [Development](docs/development.md) | `web/` |
| App authorization or native push | [App management](internal/appmanagement/README.md), [Native integration](docs/native-push.md) | `internal/appmanagement/`, `internal/native/` |
| Message content and verification codes | [HTML content](docs/html-content.md) | `internal/content/`, `internal/native/message_reference.go` |
| Persistence or schema | [Storage](docs/storage.md) | `internal/storage/`, `internal/settings/` |

## Implementation rules

- Fix the business rule at its owning layer. Use direct control flow for simple rules and introduce abstractions for concrete responsibilities.
- Use clear, affirmative English in code, comments, technical documents and logs. Keep contributor documentation and this file in English.
- Validate external input, authentication, signatures and credentials at their trust boundary once. Carry validated ownership and permissions explicitly to downstream code.
- Trust values generated and persisted by the same operation and invariants enforced by database constraints. Use natural business keys for idempotency.
- Before the first release, modify the schema baseline directly. Keep changes focused on current formats and supported behavior.
- Use concrete types or injected functions for a single implementation and its test substitutes.
- Keep user-facing messages in every catalog under `internal/i18n/locales/`; `internal/i18n/languages.json` defines the language set. English is the source. Preserve matching keys and placeholders. Business code returns stable error codes and safe parameters; presentation translates them.
- Keep credentials, message content and credential URLs out of logs and diagnostics. Follow the logging attribute whitelist.
- Maintain user procedures on the documentation site; link to them from this repository. Update the technical contract in `docs/` and relevant implementation tests together.

## Review and verification

Review each development change twice: first business behavior, authorization, lifecycle, concurrency, persistence, error semantics and regressions; then responsibilities, dependency direction, naming, control flow, duplication and necessary abstractions. Fix findings during development and recheck affected behavior. For a standalone review, report findings before making unrequested changes.

Tests assert observable behavior: HTTP status, error codes and persisted results. Name tests after behavior. Use the checks in [CONTRIBUTING.md](CONTRIBUTING.md), including focused tests and appropriate `make test`, `make check` or build verification. End-to-end smoke scripts cover the integration path. Test success complements manual contract review.

Install gitleaks and set `git config core.hooksPath .githooks` after cloning. Resolve true secrets at their source; allowlist only justified synthetic values. Report business review, design review, verification and remaining risks separately.

## Publication boundary

Core is AGPL-3.0-only; retain [LICENSE](LICENSE), [COPYRIGHT](COPYRIGHT) and third-party notices. Publish only authorized Core content. The iOS App, Platform and Relay are separate proprietary modules; integrations use explicit documented interfaces. Check licensing obligations before sharing implementation code.
