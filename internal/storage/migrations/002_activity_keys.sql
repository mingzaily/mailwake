ALTER TABLE native_pairings ADD COLUMN activity_key TEXT NOT NULL DEFAULT '';
ALTER TABLE native_pairings ADD COLUMN activity_only INTEGER NOT NULL DEFAULT 0 CHECK(activity_only IN (0,1));
ALTER TABLE native_pairings ADD COLUMN activity_version INTEGER NOT NULL DEFAULT 0 CHECK(activity_version>=0);
PRAGMA user_version=2;
