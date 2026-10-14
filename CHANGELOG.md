# Changelog

All notable changes to Mailwake Core are documented here. Versions follow [Semantic Versioning](https://semver.org); before 1.0.0, minor versions may change configuration.

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
