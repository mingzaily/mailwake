CREATE TABLE settings (
 name TEXT PRIMARY KEY,
 value TEXT NOT NULL
);
CREATE TABLE cursors (
 account TEXT NOT NULL,
 folder TEXT NOT NULL,
 checkpoint TEXT NOT NULL,
 PRIMARY KEY (account, folder)
);
CREATE TABLE outbox (
 id TEXT PRIMARY KEY,
 payload TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','dead')),
 attempts INTEGER NOT NULL DEFAULT 0,
 next_attempt INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 accepted_at INTEGER,
 last_error TEXT NOT NULL DEFAULT '',
 channel TEXT NOT NULL DEFAULT '',
 message TEXT
);
CREATE INDEX outbox_due ON outbox(state, next_attempt, created_at);
CREATE INDEX outbox_recent ON outbox(created_at DESC, id);
CREATE INDEX outbox_accepted ON outbox(accepted_at) WHERE state='accepted';

CREATE TABLE administrator (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 username TEXT NOT NULL,
 password_hash TEXT NOT NULL
);
CREATE TABLE setup_state (
 id INTEGER PRIMARY KEY CHECK (id = 1),
 code_hash TEXT NOT NULL
);
CREATE TABLE sessions (
 id_hash TEXT PRIMARY KEY,
 csrf TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 last_used_at INTEGER NOT NULL
);
CREATE TABLE api_tokens (
 id TEXT PRIMARY KEY,
 token_hash TEXT NOT NULL UNIQUE,
 name TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 last_used_at INTEGER
);
CREATE TABLE configuration (
 name TEXT PRIMARY KEY,
 encrypted BLOB NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0)
);
CREATE TABLE mailbox_subscriptions (
 account TEXT PRIMARY KEY,
 revision INTEGER NOT NULL CHECK (revision > 0),
 folders TEXT NOT NULL
);

-- unlink_pending marks a locally revoked pairing whose Relay DELETE has not succeeded yet.
CREATE TABLE native_pairings (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK (state IN ('waiting','active','expired','failed','revoked')),
 token_hash TEXT NOT NULL,
 encrypted_token BLOB,
 expires_at INTEGER NOT NULL,
 device_id TEXT NOT NULL DEFAULT '',
 signing_key TEXT NOT NULL DEFAULT '',
 encryption_key TEXT NOT NULL DEFAULT '',
 device_signature TEXT NOT NULL DEFAULT '',
 device_name TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '',
 revoked_at INTEGER NOT NULL DEFAULT 0,
 unlink_pending INTEGER NOT NULL DEFAULT 0,
 unlink_attempts INTEGER NOT NULL DEFAULT 0,
 unlink_next_at INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX native_pairings_device ON native_pairings(device_id) WHERE state='active';
CREATE INDEX native_pairings_state ON native_pairings(state, expires_at);
CREATE INDEX native_pairings_unlink ON native_pairings(unlink_next_at) WHERE unlink_pending=1;

CREATE TABLE native_deliveries (
 event_id TEXT NOT NULL REFERENCES outbox(id) ON DELETE CASCADE,
 pairing_id TEXT NOT NULL,
 device_name TEXT NOT NULL,
 envelope TEXT NOT NULL DEFAULT '',
 content_signature TEXT NOT NULL DEFAULT '',
 expires_at INTEGER NOT NULL,
 relay_id TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'pending',
 error_code TEXT NOT NULL DEFAULT '',
 accepted_at INTEGER NOT NULL DEFAULT 0,
 check_step INTEGER NOT NULL DEFAULT 0,
 next_check INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY (event_id, pairing_id)
);
CREATE INDEX native_deliveries_check ON native_deliveries(next_check) WHERE next_check>0;

-- token_hash is empty for Native-only invitations. used_at makes the token single-use.
CREATE TABLE app_invitations (
 id TEXT PRIMARY KEY,
 token_hash TEXT NOT NULL,
 management INTEGER NOT NULL,
 scopes TEXT NOT NULL,
 pairing_id TEXT NOT NULL DEFAULT '',
 expires_at INTEGER NOT NULL,
 used_at INTEGER NOT NULL DEFAULT 0,
 cancelled_at INTEGER NOT NULL DEFAULT 0,
 device_name TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL
);
CREATE TABLE app_controllers (
 id TEXT PRIMARY KEY,
 device_id TEXT NOT NULL UNIQUE,
 device_name TEXT NOT NULL,
 scopes TEXT NOT NULL,
 credential_hash TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL,
 last_used_at INTEGER NOT NULL DEFAULT 0
);
PRAGMA user_version = 1;

CREATE TABLE native_activities (
 event_id TEXT NOT NULL REFERENCES outbox(id) ON DELETE CASCADE,
 pairing_id TEXT NOT NULL,
 request TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','failed')),
 PRIMARY KEY(event_id,pairing_id)
);
