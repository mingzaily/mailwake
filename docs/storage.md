# Storage and backups

[简体中文](storage.zh-CN.md) · [Documentation](README.md)

## Data directory

Core stores its state under `MAILWAKE_DATA_DIR`. Keep the complete directory together:

- `mailwake.db`: SQLite settings, administrator access, folder checkpoints and notification outbox. SQLite may also create WAL and shared-memory files.
- `secret.key`: the key used to encrypt stored credentials and sensitive configuration.
- `core.lock`: the process lock that gives one Core instance exclusive access to the directory.

Credential encryption protects sensitive configuration inside the database. The complete directory, including the key and backups, requires private filesystem permissions.

## Database baseline

The development version creates new databases from `internal/storage/migrations/001_baseline.sql` in one transaction and records `PRAGMA user_version=1`. Existing version-1 databases open directly. Other schema versions fail with `database_version_unsupported`.

Before the first release, schema changes update the baseline directly. Development fixtures should use a fresh temporary directory when the schema changes.

## Backup and recovery

Stop Core, back up the complete directory, then restart it. Restore the database and `secret.key` from the same backup while Core is stopped. See the [deployment guide](deployment.md) for Docker backup commands, upgrades and administrator recovery.
