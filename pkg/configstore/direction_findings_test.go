package configstore

import (
	"context"
	"testing"
	"time"
)

func TestDirectionFindingObservationCRUDRoundTrip(t *testing.T) {
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	observedAt := time.Date(2026, 10, 22, 14, 30, 0, 0, time.UTC)
	observation := &DirectionFindingObservation{
		Callsign: "F4AAA-9", Latitude: 42.9, Longitude: 1.6,
		BearingDeg: 73.5, UncertaintyDeg: 8, FrequencyMHz: 121.5,
		ObservedAt: observedAt, Mission: "SATER-01", Note: "clear minimum",
	}
	if err := s.CreateDirectionFindingObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	if observation.ID == 0 {
		t.Fatal("expected assigned id")
	}

	got, err := s.GetDirectionFindingObservation(ctx, observation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Callsign != "F4AAA-9" || got.BearingDeg != 73.5 || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	got.Note = "multipath suspected"
	if err := s.UpdateDirectionFindingObservation(ctx, got); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListDirectionFindingObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Note != "multipath suspected" {
		t.Fatalf("list mismatch: %+v", all)
	}

	if err := s.DeleteDirectionFindingObservation(ctx, observation.ID); err != nil {
		t.Fatal(err)
	}
	all, err = s.ListDirectionFindingObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty list, got %+v", all)
	}
}
