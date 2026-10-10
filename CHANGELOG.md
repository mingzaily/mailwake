# Changelog

All notable changes to Mailwake Core are documented here. Versions follow [Semantic Versioning](https://semver.org); before 1.0.0, minor versions may change configuration.

## [1.0.0-rc.4] - 2026-10-10

- Refine sidebar navigation with breadcrumbs, a GitHub link, a default avatar and a desktop sidebar toggle; keep connection budgets on mailbox pages.
- Simplify panel headings, increase mailbox table spacing and align operation columns.
- Expand IMAP quick-fill presets to ten providers, with compact common shortcuts and a grouped menu for additional providers.
- Show configured-credential hints inside inputs and expand discovered folders by default.
- Localize common folder names even when servers omit special-use metadata, retaining original names beside translated labels.
- Use the final slash-separated folder segment in rendered notification titles while preserving full paths in structured payloads.
- Show shared monitoring connection errors once in the console shell.

## [1.0.0-rc.3] - 2026-10-10

- Simplify initial setup to three steps and add IMAP quick-fill presets for QQ, 163, Yahoo and Gmail.
- Localize standard mailbox folders using IMAP special-use metadata while preserving original subscription paths.
- Separate App management from notification settings and guide users to pair a phone before testing App notifications.
- Refine console layouts, account navigation, appearance controls, API token timestamps and diagnostic information.
- Default new notification configurations to the console language and localize runtime log summaries.
- Include the source commit in release binaries and container diagnostics.

## [1.0.0-rc.2] - 2026-10-10

- Add an optional 1Panel Compose example using root and a host data directory; retain the default non-root deployment.
- Report actionable startup errors for data directory permissions, read-only filesystems, storage initialization and HTTP listener failures.
- Use Mailwake consistently in the console loading screen and browser title.

## [1.0.0] - Unreleased

First public release.

### Monitoring

- Up to 20 IMAP mailboxes with stable `mbx_` IDs, display names, encrypted configuration and independent revisions.
- Per-folder checks: realtime IDLE, or scheduled every 5 or 15 minutes; scheduled folders share one connection per mailbox. Each mailbox has a connection budget (10 by default) with one connection reserved for management.
- New subscriptions start from a baseline; reconnects resume the saved UID cursor and skip mail older than 24 hours. Mailbox identity changes reset the baseline.
- Authentication failures pause logins for 30 minutes. Subjects and sender names in legacy charsets such as GBK, Big5 and Shift_JIS are decoded.
- IMAP ID is sent before discovery on servers that support it, including NetEase mailboxes.

### Delivery

- Bark, Pushover, signed webhooks (`X-Mailwake-Signature`, HMAC-SHA256) and the Mailwake App through Relay with end-to-end encryption.
- Durable SQLite outbox: folder progress and notifications commit in one transaction; one request by default with optional retries honoring `Retry-After`.
- Notification preview is `off` or `subject`; Webhook additionally carries structured sender data.

### Management

- React + shadcn console with first-run setup codes, Argon2id administrator login, CSRF-protected sessions, light/dark themes and English/Simplified Chinese.
- REST API for mailboxes, folder subscriptions, delivery settings, deliveries, logs and redacted diagnostics; revocable `mwk_` API tokens stored as hashes.
- Offline `admin reset-password`, `admin reset-setup` and `admin reset-native-push` commands.
- Optional official App management: one-time invitations with scopes, `mwac_` device credentials, and Platform qualifications for paid changes.
- Single static binary and container image for `linux/amd64` and `linux/arm64`; embedded assets with strict CSP and bundled third-party notices.
