# Mailwake Core contributor entry

## Mandatory session preflight

Use `main` for integration, `dev/*` for version development, `feat/*` for independent feature work and `hotfix/*` for urgent production fixes. Apply these project branch prefixes across all tools and sessions. Work directly on the active `dev/vX.Y.Z` for serial development. For parallel implementation or isolated validation, branch `feat/vX.Y.Z/<topic>` from the matching `dev/vX.Y.Z` into a separate worktree and integrate back into that same dev. Before implementation, state the source dev, its base SHA, the feature branch, worktree path and integration target; verify actual ancestry as well as the name. One session owns dev integration and version closeout; completed dev batches integrate into `main`.

Every session must read [Branching workflow](docs/branching.md) before starting task work, including documentation changes and reviews. Before editing, inspect the current branch, worktree status, upstream relationship and worktree occupancy, and identify the target development batch. State the selected branch and relevant risks before writing. Reuse an applicable development branch; when the checkout conflicts with the task, explain the mismatch and recommend a concrete branch/base before dependent edits. Ask only for missing decisions or ownership conflicts, and preserve other sessions' work.

## Read by task

Start with [README.md](README.md) and [CONTRIBUTING.md](CONTRIBUTING.md). This repository contains the self-hosted Core only. User tutorials live at [Mailwake documentation](https://mailwake.oritx.com/en/docs/); `docs/` contains English contributor references.

| Task | References | Owning code |
| --- | --- | --- |
| Builds, tests, fixtures or releases | [Development](docs/development.md), [Contributing](CONTRIBUTING.md) | `Makefile`, `.github/workflows/`, `web/scripts/` |
| Mailbox connections and folder watchers | [Monitoring](docs/monitoring.md) | `internal/mail/`, `internal/engine/`, `internal/runtime/` |
| Notification channels and retries | [Delivery](docs/notifications.md), [Storage](docs/storage.md) | `internal/delivery/`, `internal/storage/` |
| HTTP, sessions, tokens or localization | [API implementation](docs/api.md) | `internal/httpapi/`, `internal/auth/`, `internal/i18n/` |
| Console views or components | [Console](docs/console.md), [Development](docs/development.md) | `web/` |
| App authorization or native push | [App management](internal/appmanagement/README.md), [Native integration](docs/native-push.md) | `internal/application/`, `internal/coreidentity/`, `internal/appmanagement/`, `internal/native/` |
| Message content and verification codes | [HTML content](docs/html-content.md) | `internal/content/`, `internal/coreidentity/message_reference.go` |
| Persistence or schema | [Storage](docs/storage.md) | `internal/storage/`, `internal/settings/` |

## Implementation rules

- Git commits use Conventional Commit types and optional English scopes, with English descriptions and bodies (for example, `docs: clarify release tag conventions`).
- Fix the business rule at its owning layer. Use direct control flow for simple rules and introduce abstractions for concrete responsibilities.
- Use clear, affirmative English in code, comments, technical documents and logs. Keep contributor documentation and this file in English.
- Validate external input, authentication, signatures and credentials at their trust boundary once. Carry validated ownership and permissions explicitly to downstream code.
- Trust values generated and persisted by the same operation and invariants enforced by database constraints. Use natural business keys for idempotency.
- Before the first release, modify the schema baseline directly. Keep changes focused on current formats and supported behavior.
- Use concrete types or injected functions for a single implementation and its test substitutes.
- Keep user-facing messages in every catalog under `internal/i18n/locales/`; `internal/i18n/languages.json` defines the language set. English is the source. Preserve matching keys and placeholders. Business code returns stable error codes and safe parameters; presentation translates them.
- Keep credentials, message content and credential URLs out of logs and diagnostics. Follow the logging attribute whitelist.
- Maintain user procedures on the documentation site; link to them from this repository. Update the technical contract in `docs/` and relevant implementation tests together. Follow [documentation ownership](docs/README.md): keep current rules in one owning topic and dated verification in release records; review substance and implementation consistency as well as links.

## Review and verification

Review each development change twice: first business behavior, authorization, lifecycle, concurrency, persistence, error semantics and regressions; then responsibilities, dependency direction, naming, control flow, duplication and necessary abstractions. Fix findings during development and recheck affected behavior. For a standalone review, report findings before making unrequested changes.

Tests assert observable behavior: HTTP status, error codes and persisted results. Name tests after behavior. Use the checks in [CONTRIBUTING.md](CONTRIBUTING.md), including focused tests and appropriate `make test`, `make check` or build verification. End-to-end smoke scripts cover the integration path. Test success complements manual contract review.

Install gitleaks and set `git config core.hooksPath .githooks` after cloning. Resolve true secrets at their source; allowlist only justified synthetic values. Report business review, design review, verification and remaining risks separately.

### Feature quality gate

- Every new feature or behavior change must reach **9.0/10 in both business logic and code design** before development is marked complete. The lower score determines whether the change meets the gate.
- Business logic covers contract completeness, lifecycle and failure semantics, ownership and security, concurrency and data consistency, and regressions. Code design covers responsibilities and dependency direction, readable control flow and naming, reuse and duplication, and abstractions proportional to the problem.
- **9.0–10** means the applicable criteria have been reviewed, required development checks have passed, and remaining improvements have a documented low impact. **8.0–8.9** means a concrete correctness, maintainability or verification gap still needs work. **Below 8.0** means substantial behavior or design problems remain. Justify each score and deduction with evidence and impact.
- Score the actual result. Below 9.0, fix the root cause and repeat the affected review and verification. Open correctness, security, data-loss or contract defects, failed required checks, and missing evidence for critical changed behavior keep the gate open. Record the actual score or pending assessment until evidence supports completion.
- Assess changed behavior and affected call chains each time. Earlier scores establish a baseline. Prefer targeted fixes and measured improvements, with abstractions based on demonstrated responsibilities or extension needs.
- Report scope, both scores with reasons, checks actually run and their results, reused evidence with its revision and applicability, and remaining risks. External dependencies require explicit pending acceptance items with completion criteria. Report code readiness and release readiness separately; release-critical acceptance must pass before release.
- Update affected contracts and documentation. Preserve dated review evidence in the relevant pull request and publication evidence in GitHub Releases, including the revision, environment, coverage and limitations. Keep all contributor-facing records independently readable within this repository.

## Publication boundary

Core is AGPL-3.0-only; retain [LICENSE](LICENSE), [COPYRIGHT](COPYRIGHT) and third-party notices. Publish only authorized Core content. The iOS App, Platform and Relay are separate proprietary modules; integrations use explicit documented interfaces. Check licensing obligations before sharing implementation code.
