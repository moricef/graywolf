package configstore

import (
	"context"
	"testing"
)

func TestCreateBeaconPreservesDisabledSelection(t *testing.T) {
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, kind := range []string{"dx", "position"} {
		for _, enabled := range []bool{false, true} {
			row := Beacon{Type: kind, Callsign: "F4MLV-2", Enabled: enabled}
			if err := s.CreateBeacon(context.Background(), &row); err != nil {
				t.Fatal(err)
			}
			if row.Enabled != enabled {
				t.Fatalf("create mutated %s enabled=%v to %v", kind, enabled, row.Enabled)
			}
			got, err := s.GetBeacon(context.Background(), row.ID)
			if err != nil || got.Enabled != enabled {
				t.Fatalf("persisted %s enabled=%v got=%+v err=%v", kind, enabled, got, err)
			}
		}
	}
}
