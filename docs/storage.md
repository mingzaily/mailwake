# Storage and backups

[Contributor documentation](README.md) · [User guides](https://mailwake.oritx.com/en/docs/)

## Data directory

Core stores its state under `MAILWAKE_DATA_DIR`. Keep the complete directory together:

- `mailwake.db`: SQLite settings, administrator access, folder checkpoints and notification outbox. SQLite may also create WAL and shared-memory files.
- `secret.key`: the key used to encrypt stored credentials and sensitive configuration.
- `core.lock`: the process lock that gives one Core instance exclusive access to the directory.

Credential encryption protects sensitive configuration inside the database. The complete directory, including the key and backups, requires private filesystem permissions.

## Database baseline

The development version creates new databases from `internal/storage/migrations/001_baseline.sql` in one transaction and records `PRAGMA user_version=1`. Existing version-1 databases open directly. Other schema versions fail with `database_version_unsupported`.

Before the first release, schema changes update the baseline directly. Development fixtures should use a fresh temporary directory when the schema changes.

Activity submissions persist `failures` and `next_attempt` alongside the encrypted request and original expiry. A partial pending-work index supports the bounded due query. Terminal completion clears the encrypted request; late retry scheduling updates only pending rows. These fields belong to the initial schema baseline. Development databases created with an older baseline require a fresh data directory; the pre-release schema has no historical migration path.

## Encryption and access secrets

Each mailbox is encrypted separately under `mailbox:<id>`, with the full configuration name bound into AES-GCM authentication. Mailbox and channel settings use AES-256-GCM with a random 256-bit `secret.key` (0600). This protects a database leaked on its own; anyone with the complete directory and key can decrypt it. A missing original key makes encrypted configuration unavailable.

Administrator passwords use Argon2id with encoded parameters. Session IDs, setup codes and API tokens are stored as hashes. API tokens appear only in their creation response; the setup code appears only in its startup console line. Settings responses, diagnostics and runtime logs omit credentials. IMAP uses certificate-verified implicit TLS; notification channels use HTTPS with redirects disabled.

## Outbox payloads and history

The enqueue transaction applies the persisted [preview policy](notifications.md#privacy-and-persistence). With third-party preview `off`, tasks retain mailbox identity, display name, message location, folder and time; `subject` adds sender, optional sender_name and subject. Native payloads always include encrypted sender, optional sender_name and subject. The optional name lives in the event JSON, so the schema baseline is unchanged. Disabling preview clears the name with the address from pending and failed payloads. Message bodies remain outside persistent outbox state, history and logs.

After channel acceptance, the delivery payload is cleared. A separate history summary retains mailbox ID, display name, folder and the subject permitted at enqueue time; it excludes sender and body. Preview `off`, including the initial unconfigured state, produces `subject: null`. Existing summaries retain their recorded subjects when preview settings change. Native terminal records also clear envelopes and signatures, so native failures support diagnosis rather than manual replay.

The dispatcher prunes hourly in bounded batches: accepted records expire seven days after acceptance, and failed records expire thirty days after creation. Failed third-party payloads remain available for manual retry until removal. Manual history cleanup removes finished records while preserving active dispatch, native status checks and pending activities; see [Console](console.md#manual-history-cleanup).

## Backup and recovery

Stop Core, back up the complete directory, then restart it. Restore the database and `secret.key` from the same backup while Core is stopped. See the [deployment guide](https://mailwake.oritx.com/en/docs/backup/) for Docker backup commands, upgrades and administrator recovery.
