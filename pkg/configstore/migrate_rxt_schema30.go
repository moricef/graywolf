package configstore

import (
	"fmt"

	"gorm.io/gorm"
)

// Version 30 meant remote-command credentials in deployed RXT builds, but
// upstream assigned it to the serial KISS repair. user_version records only
// the number, so either version-30 database must receive both idempotent steps.
// Keep upstream's version-30 entry and converge both histories at version 31.
func migrateRXTSchema30Convergence(tx *gorm.DB) error {
	if err := migrateRemoteCommandAuth(tx); err != nil {
		return fmt.Errorf("remote command credentials: %w", err)
	}
	return migrateKissSerialTncDefault(tx)
}
