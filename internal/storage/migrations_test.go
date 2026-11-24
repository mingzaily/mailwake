package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReleasedDatabaseUpgradesAndPreservesPairing(t *testing.T) {
	for _, existing := range []bool{true, false} {
		name := "fresh database"
		if existing {
			name = "released version 1"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if existing {
				db, err := sql.Open("sqlite", filepath.Join(dir, "mailwake.db"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.ExecContext(t.Context(), baseline); err != nil {
					db.Close()
					t.Fatal(err)
				}
				_, err = db.ExecContext(t.Context(), "INSERT INTO native_pairings(id,state,token_hash,expires_at,device_id,signing_key,encryption_key,device_name,created_at) VALUES('paired-phone','active','',0,'phone','signing-public-key','encryption-public-key','iPhone',100)")
				if err != nil {
					db.Close()
					t.Fatal(err)
				}
				if err = db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			store, err := Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			var version int
			if err = store.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 2 {
				store.Close()
				t.Fatalf("schema version=%d error=%v", version, err)
			}
			if existing {
				pairing, err := store.NativePairing(t.Context(), "paired-phone")
				if err != nil {
					store.Close()
					t.Fatal(err)
				}
				if pairing.DeviceName != "iPhone" || pairing.EncryptionKey != "encryption-public-key" || pairing.ActivityKey != "" || pairing.ActivityOnly || pairing.ActivityVersion != 0 {
					store.Close()
					t.Fatalf("pairing changed during upgrade: %+v", pairing)
				}
				pairing.ActivityKey = "activity-public-key"
				pairing.ActivityOnly = true
				pairing.ActivityVersion = 7
				if err = store.SaveActivityPreference(t.Context(), pairing); err != nil {
					store.Close()
					t.Fatal(err)
				}
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if existing {
				pairing, err := reopened.NativePairing(t.Context(), "paired-phone")
				if err != nil || pairing.ActivityKey != "activity-public-key" || !pairing.ActivityOnly || pairing.ActivityVersion != 7 {
					t.Fatalf("upgraded pairing persistence: %+v %v", pairing, err)
				}
			}
		})
	}
}
