# Contributing to Mailwake Core

Mailwake Core is a self-contained Go module. Run the commands below from the repository root. Local development and tests use synthetic data and local test servers; production mailbox credentials are required only for manual provider validation.

Install [gitleaks](https://github.com/gitleaks/gitleaks) and enable the repository hook before committing:

```sh
git config core.hooksPath .githooks
```

The hook scans staged changes. The allowlist only covers fixed, synthetic interoperability vectors.

## Development environment

- Use the Go version declared in `go.mod`.
- Use Node.js 24.18.1 from `.nvmrc`, npm, and the committed frontend lockfile. Run `cd web && npm ci` before tests/checks.
- Enable CGO and install a C compiler for Go's race detector. Release builds use `CGO_ENABLED=0`.
- Docker is optional for local development and required for container verification.

```sh
make test
make check
make build
```

The binary is written to `bin/mailwake`. The `nomsgpack` build tag is used by the build, tests and static checks. `make test` additionally enables `mailwake_test`, which makes the low-cost password helper available to test binaries only; production builds omit this tag. `make check` checks both variants. For a focused low-cost run, use `go test -tags nomsgpack,mailwake_test ./internal/httpapi/`. A plain `go test ./internal/httpapi/` exercises production hash costs. Run `gofmt` on changed Go files.

## Directory ownership

| Path | Responsibility |
| --- | --- |
| `cmd/mailwake/` | Process startup, dependency assembly and shutdown |
| `internal/config/` | Listen address and data directory |
| `internal/auth/` | Setup, password hashes, sessions, failure limits and API tokens |
| `internal/settings/` | Mailbox/delivery models, validation, merging, redacted views, per-mailbox encryption |
| `internal/runtime/` | Independent mailbox engines/budgets, global dispatcher, channel factories and isolated hot updates |
| `internal/mail/` | Protocol-neutral sessions/shared connections, check methods and connection budgets |
| `internal/mail/imap/` | IMAP protocol handling and provider checkpoints |
| `internal/engine/` | Realtime workers, one scheduled worker per mailbox, online handovers and batch persistence |
| `internal/content/` | Message parsing, HTML to text and verification code detection |
| `internal/event/` | Notification values and stable event identities |
| `internal/storage/` | Revision CAS, per-mailbox subscriptions/progress, durable outbox |
| `internal/storage/migrations/` | Initial database baseline, applied transactionally with schema version 1 |
| `internal/delivery/` | Request attempts, retry policy, delivery state, notification text and shared HTTP rules |
| `internal/delivery/bark/`, `pushover/`, `webhook/` | Channel adapters |
| `internal/httpapi/` | Mailbox REST routes, cookies, CSRF, revision contracts, authenticated log queries and anonymous diagnostics |
| `web/` | React/TypeScript console, shadcn components, Vite and Vitest |
| `internal/httpapi/webdist/` | Ignored generated resources; only .gitkeep is tracked |
| `internal/logging/` | Whitelisted structured events, JSON stderr, 2000-entry memory ring and filtered snapshots |
| `internal/fault/` | Safe, language-independent error codes and parameters |
| `internal/i18n/locales/` | Shared English and Simplified Chinese messages |
| `internal/buildinfo/` | Release version reported by logs and diagnostics |

Keep tests beside the code they exercise. Introduce packages when a concrete responsibility needs its own boundary. Provider-specific protocol types stay inside their adapters.

## Changing behavior

Fix the rule at its owning layer and update the corresponding guide in `docs/`. Cover lifecycle, cancellation, persistence and concurrent updates when they are affected. Review business behavior first, then review code design and dependencies.

Notification attempts default to one request. Automatic retries require explicit configuration. Preserve this boundary when adding providers or changing delivery logic.

Add user-facing messages to all catalogs under `internal/i18n/locales/`, with matching keys and placeholders. English is the source catalog; `internal/i18n/languages.json` defines the nine supported languages and their native names. Business logic stores error codes and safe parameters; presentation code translates them. Runtime logs use fixed English messages, safe scalar attributes and the `logging` whitelist. Include mailbox_id on mailbox events and folder on Folder events. Keep credentials, message content, raw errors and credential URLs out of both outputs; the setup-code line uses a separate console handler. Test real sensitive fixture values against JSON stderr and the ring API.

The initial database schema is embedded from `internal/storage/migrations/001_baseline.sql`. Before the first release, update this baseline directly and use fresh temporary databases in tests. Verify transaction rollback, persisted state and unsupported-version errors. See the [storage reference](docs/storage.md).

## Private data and manual verification

Start Core with an empty data directory and create the administrator using the startup setup code. Follow the setup wizard to add mailboxes, choose each folder’s check method and configure notifications. Both mailbox and notification setup can be skipped. Keep credentials in encrypted settings; protect the whole directory and its secret.key. Share the authenticated diagnostic export when reporting runtime problems; review any additional logs before posting them.

Before a release, validate scheduled checks, realtime/scheduled switches and at least two real mailboxes' folder discovery, server-side rule movement, reconnection, isolated update/deletion and restart behavior, and verify notification labels on a real device. Automated tests use local TLS IMAP and HTTPS notification servers plus real SQLite databases.

## Project metadata

Core is licensed under GNU AGPL version 3 only (`AGPL-3.0-only`); see
[LICENSE](LICENSE) and [COPYRIGHT](COPYRIGHT). Contributions to Core are
submitted under the same license, with contributors retaining their copyrights.

Mailwake Core is the open-source part of Mailwake. The Mailwake iOS app, the
push relay and Platform are separate proprietary modules that talk
to Core only through documented interfaces; their code is not part of this
repository. Third-party code retains its own license and attribution
requirements.

Pull requests are welcome after the change has been discussed in an issue. A
contributor license agreement will be required before external pull requests
are merged; it will be linked here.

Distributions of binaries or container images include the license, applicable
third-party notices and access to the corresponding source for that release.

See the [development guide](docs/development.md) for local fixtures and the [console guide](docs/console.md) for component and CSP rules.

## Management UI verification

`make test` runs Vitest and Go race tests. `make check` runs TypeScript, ESLint and Go vet; `make build` installs locked npm dependencies, builds the UI, audits licenses and embeds the result. Plain Go commands embed `all:webdist` and use the separate `placeholder.html` when the built index is absent. Only `webdist/.gitkeep` is tracked; the build restores that marker after clearing generated output. In a Git checkout, run `make check-build` to build and verify tracked assets remain unchanged, or `make check-webdist` for just the Git check.

Run `npm run dev` in `web/` against a temporary Core on 8080 (or set `MAILWAKE_DEV_TARGET` to another temporary instance). The proxy preserves Host/Origin and session cookies. Verify production CSP using the built binary: development transforms do not represent its CSP behavior. Frontend error tests cover 401/400/409/429, CSRF headers, input retention, subscription budgets, preview values and translation substitution.

Keep all UI copy in every shared Go catalog under `ui.` keys. Use semantic tokens, small radii, Geist fonts and the supplied console mockup. Keep scripts/styles/fonts local and avoid inline script/style/style attributes. Static toast markup preserves this policy. Retain upstream shadcn attribution. The license audit includes build dependencies; the MIT Tailwind compiler/scanner adapter avoids introducing the official adapter’s MPL dependency. ESLint Hooks retains its pre-compiler rule set under an explicit ESLint peer override; its rules run in `make check`.

Walk through setup, login, every page and logout with synthetic accounts. Cover stale revisions, wrong current password, test saturation, mixed check methods, write-only credentials, log pause/filter/export, one-time token values and inline deletion/revocation. Capture every page in dark/light at 1440px, plus overview/mailboxes/logs at 390px. Inspect browser errors and verify zero CSP violations, inline styles or external resources. Store verification screenshots and temporary credentials outside the repository. README screenshots in `docs/images/` use synthetic data only. Real provider and device validation remains a release check.

## Release to GHCR

Push a version tag such as `v1.0.0` after release acceptance. The Release workflow first runs CI, then publishes:

- Multi-platform images for `linux/amd64` and `linux/arm64` to `ghcr.io/mingzaily/mailwake`, authenticated with the workflow's `GITHUB_TOKEN` and `packages: write`.
- Version tags (`v1.0.0`, `v1.0`) and `latest` for stable releases; prerelease tags keep their prerelease version and leave `latest` unchanged.
- Linux/macOS amd64/arm64 binaries with the embedded Web console, deployment documents, license notices and SHA256 checksums in GitHub Releases.

GHCR is the only image publication destination. The Dockerfile downloads its upstream base images from Docker Hub. After the first GHCR publication, set the package visibility to Public in GitHub package settings and verify an anonymous pull of the versioned image. Public repository visibility and package visibility are separate settings.

The public `main` branch is buildable development source. Create release tags after real mailbox and device acceptance; the first public source commit does not itself create a release.
