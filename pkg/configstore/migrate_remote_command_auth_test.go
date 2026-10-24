package configstore

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateRemoteCommandAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := migrateRemoteCommandAuth(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var count int
	if err := db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='remote_command_credentials'`).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("remote_command_credentials table missing")
	}
	if err := db.Exec(`INSERT INTO remote_command_credentials
		(name, target_call, secret_b64url, key_id, last_counter, created_at, updated_at)
		VALUES ('one', 'F4MLV-15', 'secret', 'A', 0, datetime('now'), datetime('now'))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO remote_command_credentials
		(name, target_call, secret_b64url, key_id, last_counter, created_at, updated_at)
		VALUES ('two', 'F4MLV-15', 'secret', 'A', 0, datetime('now'), datetime('now'))`).Error; err == nil {
		t.Fatal("duplicate target_call was accepted")
	}
}

func TestSignedTargetUpgradeFromSchema31(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema31.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct a deployed schema-31 credential table, without signed_target.
	if err := s.DB().Exec("DROP TABLE remote_command_credentials").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateRemoteCommandAuth(s.DB()); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec(`INSERT INTO remote_command_credentials
		(name,target_call,secret_b64url,key_id,last_counter,created_at,updated_at,last_used_at)
		VALUES ('existing','BURNTX','preserved-secret','A',70,'2026-10-23','2026-10-23','2026-10-23')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Exec("PRAGMA user_version=31").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			TargetCall, SignedTarget, SecretB64url, KeyID string
			LastCounter                                   int
			LastUsedAt                                    *string
		}
		if err := s.DB().Raw("SELECT target_call,signed_target,secret_b64url,key_id,last_counter,CAST(last_used_at AS TEXT) AS last_used_at FROM remote_command_credentials").Scan(&row).Error; err != nil {
			t.Fatal(err)
		}
		want := "BURNTX"
		if attempt == 1 {
			want = "F4MLV-10"
		}
		if row.TargetCall != "BURNTX" || row.SignedTarget != want || row.SecretB64url != "preserved-secret" || row.KeyID != "A" || row.LastCounter != 70 || row.LastUsedAt == nil {
			t.Fatalf("migration changed credential: %+v", row)
		}
		if err := s.DB().Exec("UPDATE remote_command_credentials SET signed_target='F4MLV-10'").Error; err != nil {
			t.Fatal(err)
		}
		if err := migrateRemoteCommandSignedTarget(s.DB()); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
