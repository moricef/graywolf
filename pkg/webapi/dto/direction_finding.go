package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// DirectionFindingRequest describes a manual bearing reported by a field
// operator. BearingDeg is a true bearing clockwise from north.
type DirectionFindingRequest struct {
	Callsign       string    `json:"callsign"`
	Mission        string    `json:"mission"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	BearingDeg     float64   `json:"bearing_deg"`
	UncertaintyDeg float64   `json:"uncertainty_deg"`
	FrequencyMHz   float64   `json:"frequency_mhz"`
	ObservedAt     time.Time `json:"observed_at"`
	Note           string    `json:"note"`
}

func (r DirectionFindingRequest) Validate() error {
	if strings.TrimSpace(r.Callsign) == "" {
		return fmt.Errorf("callsign is required")
	}
	if len(strings.TrimSpace(r.Callsign)) > 20 {
		return fmt.Errorf("callsign is too long")
	}
	if len(strings.TrimSpace(r.Mission)) > 80 {
		return fmt.Errorf("mission is too long")
	}
	if len(strings.TrimSpace(r.Note)) > 500 {
		return fmt.Errorf("note is too long")
	}
	if r.Latitude < -90 || r.Latitude > 90 {
		return fmt.Errorf("latitude out of range")
	}
	if r.Longitude < -180 || r.Longitude > 180 {
		return fmt.Errorf("longitude out of range")
	}
	if r.BearingDeg < 0 || r.BearingDeg >= 360 {
		return fmt.Errorf("bearing_deg must be in [0, 360)")
	}
	if r.UncertaintyDeg < 0 || r.UncertaintyDeg > 90 {
		return fmt.Errorf("uncertainty_deg must be in [0, 90]")
	}
	if r.FrequencyMHz <= 0 || r.FrequencyMHz > 10000 {
		return fmt.Errorf("frequency_mhz out of range")
	}
	if r.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at is required")
	}
	return nil
}

func (r DirectionFindingRequest) ToModel() configstore.DirectionFindingObservation {
	return configstore.DirectionFindingObservation{
		Callsign:       strings.ToUpper(strings.TrimSpace(r.Callsign)),
		Mission:        strings.TrimSpace(r.Mission),
		Latitude:       r.Latitude,
		Longitude:      r.Longitude,
		BearingDeg:     r.BearingDeg,
		UncertaintyDeg: r.UncertaintyDeg,
		FrequencyMHz:   r.FrequencyMHz,
		ObservedAt:     r.ObservedAt.UTC(),
		Note:           strings.TrimSpace(r.Note),
	}
}

func (r DirectionFindingRequest) ToUpdate(id uint32) configstore.DirectionFindingObservation {
	m := r.ToModel()
	m.ID = id
	return m
}

type DirectionFindingResponse struct {
	ID        uint32    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DirectionFindingRequest
}

func DirectionFindingFromModel(m configstore.DirectionFindingObservation) DirectionFindingResponse {
	return DirectionFindingResponse{
		ID:        m.ID,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
		DirectionFindingRequest: DirectionFindingRequest{
			Callsign:       m.Callsign,
			Mission:        m.Mission,
			Latitude:       m.Latitude,
			Longitude:      m.Longitude,
			BearingDeg:     m.BearingDeg,
			UncertaintyDeg: m.UncertaintyDeg,
			FrequencyMHz:   m.FrequencyMHz,
			ObservedAt:     m.ObservedAt,
			Note:           m.Note,
		},
	}
}
