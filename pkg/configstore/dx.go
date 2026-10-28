package configstore

import (
	"context"
	"errors"
	"time"

	"github.com/chrissnell/graywolf/pkg/rfdx"
	"gorm.io/gorm"
)

type DXSettings struct {
	ID          uint32 `gorm:"primaryKey" json:"-"`
	rfdx.Config `gorm:"embedded"`
}

type DXRecord struct {
	Channel    uint32 `gorm:"primaryKey"`
	Callsign   string
	DistanceKM float64
	BearingDeg float64
	ReceivedAt time.Time
}

func (s *Store) GetDXSettings(ctx context.Context) (rfdx.Config, error) {
	var row DXSettings
	err := s.db.WithContext(ctx).First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return rfdx.DefaultConfig(), nil
	}
	return row.Config, err
}

func (s *Store) SaveDXSettings(ctx context.Context, c rfdx.Config) error {
	return s.db.WithContext(ctx).Save(&DXSettings{ID: 1, Config: c}).Error
}

func (s *Store) LoadDXRecords(ctx context.Context) ([]rfdx.Result, error) {
	var rows []DXRecord
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []rfdx.Result
	for _, r := range rows {
		out = append(out, rfdx.Result{Channel: r.Channel, Callsign: r.Callsign, DistanceKM: r.DistanceKM, BearingDeg: r.BearingDeg, ReceivedAt: r.ReceivedAt})
	}
	return out, nil
}

func (s *Store) SaveDXRecords(ctx context.Context, records []rfdx.Result) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, r := range records {
			if err := tx.Save(&DXRecord{Channel: r.Channel, Callsign: r.Callsign, DistanceKM: r.DistanceKM, BearingDeg: r.BearingDeg, ReceivedAt: r.ReceivedAt}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ClearDXRecords(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("1 = 1").Delete(&DXRecord{}).Error
}

// Transfer the earlier local test-build report out of the user beacon list.
// Preserve the old rows disabled for review; never activate an inactive report.
func (s *Store) seedDXSettings(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var saved DXSettings
		if err := tx.First(&saved, 1).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var b Beacon
		if err := tx.Where("type = ?", "dx").Order("id").First(&b).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		c := rfdx.DefaultConfig()
		c.Channel = b.Channel
		c.CapabilitiesEnabled = b.Enabled
		c.SendPath = b.SendPath
		c.Path = b.Path
		if c.SendPath == "" {
			c.SendPath = "rf"
		}
		if b.EverySeconds >= 60 {
			c.CapabilitiesIntervalMinutes = b.EverySeconds / 60
		}
		if c.CapabilitiesIntervalMinutes > 1440 {
			c.CapabilitiesIntervalMinutes = 1440
		}
		if err := tx.Save(&DXSettings{ID: 1, Config: c}).Error; err != nil {
			return err
		}
		return tx.Model(&Beacon{}).Where("type = ?", "dx").Update("enabled", false).Error
	})
}
