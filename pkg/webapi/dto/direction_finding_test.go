package dto

import (
	"testing"
	"time"
)

func validDirectionFindingRequest() DirectionFindingRequest {
	return DirectionFindingRequest{
		Callsign: " f4aaa-9 ", Latitude: 42.9, Longitude: 1.6,
		BearingDeg: 0, UncertaintyDeg: 10, FrequencyMHz: 121.5,
		ObservedAt: time.Date(2026, 10, 22, 14, 30, 0, 0, time.UTC),
	}
}

func TestDirectionFindingRequestValidationAndNormalization(t *testing.T) {
	req := validDirectionFindingRequest()
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	m := req.ToModel()
	if m.Callsign != "F4AAA-9" {
		t.Fatalf("callsign = %q", m.Callsign)
	}

	cases := []struct {
		name string
		edit func(*DirectionFindingRequest)
	}{
		{"empty callsign", func(r *DirectionFindingRequest) { r.Callsign = " " }},
		{"latitude", func(r *DirectionFindingRequest) { r.Latitude = 91 }},
		{"longitude", func(r *DirectionFindingRequest) { r.Longitude = -181 }},
		{"negative bearing", func(r *DirectionFindingRequest) { r.BearingDeg = -1 }},
		{"full-circle bearing", func(r *DirectionFindingRequest) { r.BearingDeg = 360 }},
		{"uncertainty", func(r *DirectionFindingRequest) { r.UncertaintyDeg = 91 }},
		{"frequency", func(r *DirectionFindingRequest) { r.FrequencyMHz = 0 }},
		{"time", func(r *DirectionFindingRequest) { r.ObservedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validDirectionFindingRequest()
			tc.edit(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
