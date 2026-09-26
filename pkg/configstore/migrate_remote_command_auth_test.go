package configstore

import (
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
