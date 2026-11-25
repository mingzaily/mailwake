ALTER TABLE app_controllers ADD COLUMN omit_activity_notification INTEGER NOT NULL DEFAULT 0 CHECK(omit_activity_notification IN (0,1));
ALTER TABLE native_deliveries ADD COLUMN pull_pending INTEGER NOT NULL DEFAULT 0 CHECK(pull_pending IN (0,1));
CREATE INDEX native_deliveries_pull ON native_deliveries(pairing_id,accepted_at,event_id) WHERE pull_pending=1;
PRAGMA user_version=3;
