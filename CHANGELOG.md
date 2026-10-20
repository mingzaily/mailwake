# Changelog

All notable changes to Mailwake Core are documented here. Versions follow [Semantic Versioning](https://semver.org); before 1.0.0, minor versions may change configuration.

## [1.0.0-rc.4] - 2026-10-20

- Add `POST /api/v1/app/mailboxes/scan` to validate a mailbox draft and return selectable folders and display metadata before saving. Require both mailbox and folder permissions and keep the probe free of persisted configuration and active watchers.
- Accept initial folder subscriptions when creating a mailbox through the Web/script and App APIs. Validate check methods, connection budgets and the current server directory, then save encrypted credentials and subscriptions in one transaction before starting monitoring.
- Preserve mailbox-only creation with empty subscriptions and cover scope isolation, invalid folder selections and transactional rollback.

This is a prerelease candidate for the App's scan-select-save flow. Validate discovery, cancellation, selected-folder persistence and first notification delivery with the matching client and real providers. See [release verification](docs/releases.md#rc4-candidate--2026-10-20).

## [1.0.0-rc.3] - 2026-10-19

- Add `POST /api/v1/app/content/read` for clients to mark an opened body as read, including cached bodies. Validate the signed reference, current account and subscription, and permanent IMAP `\Seen` permission; preserve other flags and watcher progress.
- Preserve the From display name as optional `sender_name` through events, the outbox, encrypted notifications and activities. Keep `sender` as the complete mailbox address and clear both fields under the preview privacy policy.
- Share native payload construction and cover signed references, Unicode/JSON escaping, truncation order and fixed-field size failures with regression tests.

This is a prerelease candidate for client and real-provider acceptance. Clients must implement the read action; notification delivery and body downloads alone preserve the mailbox's unread state. Clients can decode `sender_name` optionally and fall back to `sender`. Live Gmail/QQ flag persistence, preserved star flags, continued monitoring, cached-body retries and device presentation remain pending, alongside the earlier preview-transition checks. See [release verification](docs/releases.md#rc3-candidate--2026-10-19).

## [1.0.0-rc.2] - 2026-10-16

- Preserve complete IMAP FETCH responses across split metadata, BODY-before-UID ordering and cancellation; keep checkpoints unchanged when existing messages have incomplete metadata.
- Apply the persisted preview policy when enqueueing notifications and distinguish an unavailable historical subject from an actual empty subject.
- Bind saved mailbox passwords and Bark device keys to their configured targets; changing the target requires explicit replacement credentials.
- Improve console loading, stale-data handling, folder labels and login feedback while preserving unsaved edits and authorization boundaries.
- Maintain the patched MIT-licensed go-imap source under `deps/`, with reproducible builds, race tests, vet and license notices.
- Consolidate contributor contracts, release evidence and outpost terminology. Include the maintained dependency's documentation in binary archives.

This is a prerelease candidate for real-provider and device validation. Gmail/QQ monitoring and body reading, and Gmail/Bark preview transitions still require live acceptance. Existing history whose subject was already stored as an empty string cannot be reconstructed. See [release verification](docs/releases.md#rc2-candidate--2026-10-16).

## [1.0.0-rc.1] - 2026-10-14

This release starts a new prerelease baseline using the complete current Core implementation. Earlier development RC numbers have been retired. The code is preserved; see the [release record](docs/releases.md) for history and verification details.

- Monitor up to 20 IMAP mailboxes with encrypted credentials, independent connection budgets, realtime IDLE and scheduled folder checks. Persist folder progress and notifications atomically.
- Deliver through Bark, Pushover, signed Webhooks or end-to-end encrypted Mailwake App notifications. Keep explicit tests separate from saved configuration and ordinary notification retries.
- Authorize App access with local device credentials, owner-approved scopes and resource permissions. Body reading is free; the official App controls management Pro access and Relay checks Native Push eligibility.
- Return sanitized HTML, plain-text fallback and remote-image metadata. Use bounded BODY.PEEK reads, validate same-message CID images and leave remote image loading to iOS.
- Synchronize revoked push pairings with Relay and clear a revoked selected notification target atomically. Preserve independent management permissions, other channels and delivery history.
- Persist activity submission backoff and expiry across restarts, independently of ordinary notification outcomes.
- Provide a nine-language Web console with setup, mailbox and folder management, App authorization, delivery history, logs, tokens and diagnostics. Protect edits during saves and load pages on demand.
- Separate service assembly, local identity, HTTP capabilities and safe configuration views. Keep contributor references in Core and publish user tutorials on the documentation site.
- Distribute Linux/macOS amd64/arm64 binaries and Linux amd64/arm64 containers with the embedded console, licenses and checksums.

The development schema uses a single baseline. Development databases created before the current baseline require a fresh data directory; see [storage and backups](docs/storage.md).
