package configstore

import (
	"path/filepath"
	"testing"
)

func TestSchema30UpgradeFromBothHistories(t *testing.T) {
	for _, history := range []string{"upstream", "rxt"} {
		t.Run(history, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "schema30.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			// Reconstruct the two already-deployed version-30 histories.
			if history == "upstream" {
				err = s.DB().Exec("DROP TABLE remote_command_credentials").Error
			} else {
				err = s.DB().Exec(`INSERT INTO remote_command_credentials
					(name, target_call, secret_b64url, key_id, last_counter, created_at, updated_at)
					VALUES ('digi', 'F1ZDB-13', 'preserved-secret', 'B', 42, datetime('now'), datetime('now'))`).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			mode, allow := KissModeTnc, 1
			if history == "rxt" {
				mode, allow = KissModeModem, 0
			}
			if err := s.DB().Exec(`INSERT INTO kiss_interfaces
				(name, interface_type, channel, mode, allow_tx_from_governor, created_at, updated_at)
				VALUES ('serial-link', 'serial', 2, ?, ?, datetime('now'), datetime('now'))`, mode, allow).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB().Exec("PRAGMA user_version=30").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			for reopen := 0; reopen < 2; reopen++ {
				s, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				var version, allow int
				var mode string
				if err := s.DB().Raw("PRAGMA user_version").Scan(&version).Error; err != nil {
					t.Fatal(err)
				}
				if version != highestMigrationVersion(t) {
					t.Fatalf("user_version=%d", version)
				}
				if err := s.DB().Raw("SELECT mode FROM kiss_interfaces WHERE name='serial-link'").Scan(&mode).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.DB().Raw("SELECT allow_tx_from_governor FROM kiss_interfaces WHERE name='serial-link'").Scan(&allow).Error; err != nil {
					t.Fatal(err)
				}
				if mode != KissModeTnc || allow != 1 {
					t.Fatalf("serial link: mode=%q allow=%d", mode, allow)
				}
				if !s.DB().Migrator().HasTable("remote_command_credentials") {
					t.Fatal("remote command credentials table missing")
				}
				if history == "rxt" {
					var credential struct {
						Secret  string
						KeyID   string
						Counter int
					}
					if err := s.DB().Raw(`SELECT secret_b64url AS secret, key_id, last_counter AS counter
						FROM remote_command_credentials WHERE target_call='F1ZDB-13'`).Scan(&credential).Error; err != nil {
						t.Fatal(err)
					}
					if credential.Secret != "preserved-secret" || credential.KeyID != "B" || credential.Counter != 42 {
						t.Fatalf("credential changed: %+v", credential)
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
