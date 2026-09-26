package configstore

import "gorm.io/gorm"

// migrateRemoteCommandAuth creates the sender-side credential and persistent
// counter table for CA2RXU !RC1 authenticated remote commands. Secrets never
// leave the server through a read endpoint. The counter is advanced before a
// message is submitted so a crash can consume, but never reuse, a value.
func migrateRemoteCommandAuth(tx *gorm.DB) error {
	return tx.Exec(`CREATE TABLE IF NOT EXISTS remote_command_credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		target_call TEXT NOT NULL UNIQUE,
		secret_b64url TEXT NOT NULL,
		key_id TEXT NOT NULL DEFAULT 'A',
		last_counter INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_used_at DATETIME
	)`).Error
}
