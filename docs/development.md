# Development guide

[简体中文](development.zh-CN.md) · [Documentation](README.md)

## Local development

Development uses Go and Node.js 24.18.1. Run `cd web && npm ci` once before checks or tests; running Core needs only the binary.

```sh
make web      # npm ci, TypeScript, Vite, license audit and notices
make test     # Vitest and Go race tests
make check    # TypeScript, ESLint and Go vet
make build    # bin/mailwake; set VERSION=v1.0.0 to stamp a release version
```

For frontend development, run Core in a new temporary data directory, then open the Vite address:

```sh
MAILWAKE_DATA_DIR="$(mktemp -d)" ./bin/mailwake
# In another terminal:
cd web
npm ci
npm run dev
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` and `/locales` to `127.0.0.1:8080`, preserving Host and Origin (`changeOrigin: false`). Setup/login cookies belong to the Vite origin; mutations send the in-memory CSRF token. If 8080 is occupied by an existing Core, use a separate temporary instance and `MAILWAKE_DEV_TARGET=http://127.0.0.1:18080 npm run dev`. Keep real data out of UI verification. Vite's development transforms are for local iteration; verify production CSP with `make build` and its binary.

Generated `internal/httpapi/webdist/` assets are excluded from commits. Git tracks only `webdist/.gitkeep`; `go:embed all:webdist` supports plain Go builds. When the built index is absent, `/` serves the separately embedded `internal/httpapi/placeholder.html`, which asks you to run `make web`. `make check-build` runs the build and checks that tracked files under webdist remain unchanged; `make check-webdist` runs only the Git check. Docker builds the UI in a separate Node stage; the final image contains no Node runtime.

All locked dependency licenses are audited against MIT, ISC, Apache-2.0, BSD, OFL and BlueOak-1.0.0. The minimatch build dependency uses [Blue Oak 1.0.0](https://blueoakcouncil.org/license/1.0.0); its full license text is included in generated notices. Tailwind's MIT compiler and scanner are used directly to keep its MPL build adapter outside the dependency graph. Static toast markup replaces sonner's injected styles. The shadcn source attribution is retained in `web/SHADCN-LICENSE` and generated notices.

Tests use local TLS IMAP servers, real SQLite databases and local HTTPS notification receivers. They cover folder monitoring and restart recovery, mailbox isolation, concurrent configuration updates, delivery retries, webhook signatures, authentication and log privacy. Real provider rules and device display require manual validation with your own accounts.

```text
cmd/mailwake/          configuration, assembly, startup and shutdown
internal/
  config/              listen address and data directory
  auth/                setup, passwords, sessions, rate limits and API tokens
  settings/            mailbox/delivery models, validation, per-mailbox encryption
  runtime/             per-mailbox engines/budgets, global dispatcher and isolated hot updates
  mail/                mail source and folder session contracts
    imap/              go-imap connections, IDLE and UID checkpoints
  engine/              realtime workers, shared-connection scheduler, progress and status
  content/             message parsing, HTML to text and verification codes
  event/               notification values and stable identifiers
  fault/               language-independent error codes and parameters
  i18n/locales/        en.json and zh-CN.json shared by backend and frontend
  storage/             revision CAS, scoped subscriptions/checkpoints, outbox and database baseline
  logging/             safe JSON logs, bounded memory ring and query snapshots
  delivery/            dispatch, retries, results, notification text and HTTP rules
    bark/ pushover/ webhook/   channel adapters
  httpapi/             mailbox REST API, cookies, CSRF and anonymous diagnostics
    webdist/           ignored generated assets and tracked .gitkeep
  buildinfo/           release version reporting
web/                   React/TypeScript source, components, Vitest and Vite build
```

See [CONTRIBUTING.md](../CONTRIBUTING.md) for conventions, [CHANGELOG.md](../CHANGELOG.md) for releases and [SECURITY.md](../SECURITY.md) to report a vulnerability. Request fields follow the [Bark API](https://github.com/Finb/bark-server/blob/master/docs/API_V2.md) and the [Pushover API](https://pushover.net/api).

Current scope includes password or app-password login, up to 20 IMAP mailboxes and one global notification channel. Free provides self-deployed monitoring and BYO delivery; Pro provides official App management and Native notifications. Gmail uses TLS IMAP and an app password. Microsoft Graph is listed as unavailable until its backend is implemented. go-imap/v2 is pinned to beta.8; please report provider compatibility issues.
## Isolated local TLS fixtures

The selfhost process accepts explicit test CA files for local TLS IMAP, Native Relay and BYO delivery integration. Configure `MAILWAKE_LOCAL_TEST_TLS=1`, then independently set `MAILWAKE_LOCAL_TEST_IMAP_CA_FILE`, `MAILWAKE_LOCAL_TEST_RELAY_CA_FILE` and/or `MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE` to an absolute PEM CA file path. With the switch absent or `0`, CA-file configuration is rejected; with the switch `1`, at least one valid CA file is required. Files must be regular files up to 64 KiB and contain CA certificate PEM blocks; symlinks, private keys, leaf certificates and unrelated data fail startup with `config_local_test_tls_invalid`.

These startup snapshots use separate IMAP, Relay and Delivery pools. IMAP test roots apply only to `localhost`, loopback IPs and `.test` names; other IMAP hosts retain system trust. A Relay test pool requires an HTTPS Relay URL in the same host range. Delivery test roots apply only to HTTPS test hosts for Bark, Pushover and Webhook; public targets retain system trust. Pushover's user-facing endpoint stays fixed at its official URL. Every adapter verifies the certificate against the exact configured hostname/IP and keeps TLS 1.2 or later. The process keeps system trust unchanged and installs fixture roots through explicit files on Darwin. Test trust for outbound connections is separate from Core App HTTPS origin and Platform qualification trust.

```sh
MAILWAKE_LOCAL_TEST_TLS=1
MAILWAKE_LOCAL_TEST_IMAP_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_LOCAL_TEST_RELAY_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_LOCAL_TEST_DELIVERY_CA_FILE=/absolute/path/to/test-root-ca.pem
MAILWAKE_RELAY_URL=https://127.0.0.1:19444
```

For an IMAP fixture at `localhost:11993`, its certificate must include the `localhost` DNS SAN; the Relay above requires the `127.0.0.1` IP SAN. IMAP AUTH uses the configured username unchanged. Relay and BYO redirects stay disabled and failures retain safe channel/protocol codes. The three independent file settings may explicitly install the same fixture CA. Runtime injection uses `Options.IMAPRootCAs`, `Options.RelayRootCAs` and `Options.DeliveryRootCAs`; Delivery roots are passed through the existing sender HTTP clients. Channel configuration keeps its existing HTTPS/target rules. Production leaves all four local-test variables unset.
