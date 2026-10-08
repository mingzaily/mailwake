# Changelog

All notable changes to Mailwake Core are documented here. Versions follow [Semantic Versioning](https://semver.org); before 1.0.0, minor versions may change configuration.

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
