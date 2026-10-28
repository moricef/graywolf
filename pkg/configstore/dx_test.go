package configstore

import (
	"context"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"path/filepath"
	"testing"
	"time"
)

func TestDXSettingsAndRecordsSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dx.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := rfdx.DefaultConfig()
	c.Channel = 3
	c.WindowMinutes = 90
	c.IgnoredCalls = "F4MLV-2"
	c.CapabilitiesEnabled = true
	if err = s.SaveDXSettings(ctx, c); err != nil {
		t.Fatal(err)
	}
	r := rfdx.Result{Channel: 3, Callsign: "RECORD", DistanceKM: 139, ReceivedAt: time.Now().UTC()}
	if err = s.SaveDXRecords(ctx, []rfdx.Result{r}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetDXSettings(ctx)
	if err != nil || got != c {
		t.Fatalf("settings: %+v %v", got, err)
	}
	records, err := s.LoadDXRecords(ctx)
	if err != nil || len(records) != 1 || records[0].Callsign != "RECORD" {
		t.Fatalf("records: %+v %v", records, err)
	}
	if err = s.ClearDXRecords(ctx); err != nil {
		t.Fatal(err)
	}
	if records, _ = s.LoadDXRecords(ctx); len(records) != 0 {
		t.Fatal(records)
	}
}

func TestLegacyDXReportTransferredWithoutCreatingPositionBeacon(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s, err := OpenMemory()
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		old := Beacon{Type: "dx", Channel: 2, SendPath: "is_only", Enabled: enabled, EverySeconds: 3600}
		if err = s.CreateBeacon(ctx, &old); err != nil {
			t.Fatal(err)
		}
		if err = s.seedDXSettings(ctx); err != nil {
			t.Fatal(err)
		}
		got, _ := s.GetDXSettings(ctx)
		if got.Channel != 2 || got.CapabilitiesEnabled != enabled || got.StatusEnabled || got.CapabilitiesIntervalMinutes != 60 {
			t.Fatalf("migration: %+v", got)
		}
		archived, _ := s.GetBeacon(ctx, old.ID)
		if archived.Enabled {
			t.Fatal("old report still active")
		}
		got.StatusEnabled = true
		s.SaveDXSettings(ctx, got)
		if err = s.seedDXSettings(ctx); err != nil {
			t.Fatal(err)
		}
		again, _ := s.GetDXSettings(ctx)
		if !again.StatusEnabled {
			t.Fatal("migration overwrote edited settings")
		}
		s.Close()
	}
}
