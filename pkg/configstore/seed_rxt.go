package configstore

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

func (s *Store) GetRXTConfig(ctx context.Context) (RXTConfig, error) {
	var c RXTConfig
	err := s.db.WithContext(ctx).Order("id").First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RXTConfig{}, nil
	}
	return c, err
}

func (s *Store) UpsertRXTConfig(ctx context.Context, c RXTConfig) error {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	db := s.db.WithContext(ctx)
	var existing RXTConfig
	err := db.Order("id").First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		updates := map[string]any{"endpoint": c.Endpoint}
		if c.Endpoint != existing.Endpoint {
			updates["last_event_id"] = ""
			updates["last_boot_id"] = ""
			updates["resume_supported"] = false
		}
		return db.Model(&RXTConfig{}).Where("id = ?", existing.ID).Updates(updates).Error
	}
	return db.Create(&RXTConfig{Endpoint: c.Endpoint}).Error
}

// UpdateRXTResume persists a stream cursor only when the endpoint still
// matches. A concurrent endpoint change must never attach an old producer's
// cursor to the newly configured source.
func (s *Store) UpdateRXTResume(ctx context.Context, endpoint, eventID, bootID string, supported bool) error {
	return s.db.WithContext(ctx).Model(&RXTConfig{}).
		Where("endpoint = ?", strings.TrimSpace(endpoint)).
		Updates(map[string]any{
			"last_event_id": eventID, "last_boot_id": bootID,
			"resume_supported": supported,
		}).Error
}
