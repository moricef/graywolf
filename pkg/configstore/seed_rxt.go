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
		return db.Model(&RXTConfig{}).Where("id = ?", existing.ID).
			Update("endpoint", c.Endpoint).Error
	}
	return db.Create(&RXTConfig{Endpoint: c.Endpoint}).Error
}
