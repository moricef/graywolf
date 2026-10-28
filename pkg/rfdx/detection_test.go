package rfdx

import (
	"strings"
	"testing"
	"time"
)

func TestDXFilteringTieRecordAndReset(t *testing.T) {
	tr, now := testTracker()
	c := DefaultConfig()
	c.MinDistanceKM = 150
	c.IgnoredCalls = "IGNORED"
	tr.Configure(c)
	tr.Record(packet("CLOSE", 1, 44))
	tr.Record(packet("IGNORED", 1, 47))
	p := packet("NULL", 1, 0)
	p.Position.Longitude = 0
	tr.Record(p)
	if len(tr.Snapshot().Channels) != 0 {
		t.Fatal("minimum distance, ignore or null-position filter failed")
	}
	tr.Record(packet("FIRST", 1, 45))
	*now = now.Add(time.Second)
	tr.Record(packet("TIE", 1, 45))
	s := tr.Snapshot()
	if len(s.Channels) != 1 || s.Channels[0].Callsign != "TIE" || len(s.MaxEver) != 1 {
		t.Fatalf("latest equal distance should win: %+v", s)
	}
	*now = now.Add(time.Second)
	tr.ClearRecords()
	if s := tr.Snapshot(); len(s.MaxEver) != 0 || len(s.Channels) != 1 {
		t.Fatalf("reset must preserve current DX without recreating record: %+v", s)
	}
	*now = now.Add(61 * time.Minute)
	if len(tr.Snapshot().Channels) != 0 {
		t.Fatal("window did not expire")
	}
	tr.RestoreRecords([]Result{{Channel: 2, Callsign: "PERSIST", DistanceKM: 300}})
	if tr.Snapshot().MaxEver[0].Callsign != "PERSIST" {
		t.Fatal("record restore failed")
	}
}

func TestDXNotificationThresholdAndInterval(t *testing.T) {
	tr, now := testTracker()
	c := DefaultConfig()
	c.Channel = 1
	c.NotifyEnabled = true
	c.StatusEnabled = true
	c.MinTriggerKM = 150
	tr.Configure(c)
	tr.Record(packet("CLOSE", 1, 44))
	if len(tr.TakeNotifications()) != 0 {
		t.Fatal("below trigger")
	}
	tr.Record(packet("MOBILE", 1, 45))
	if len(tr.TakeNotifications()) != 1 {
		t.Fatal("first qualifying DX should notify")
	}
	*now = now.Add(time.Minute)
	tr.Record(packet("MOBILE", 1, 46))
	if len(tr.TakeNotifications()) != 0 {
		t.Fatal("same moving station bypassed interval")
	}
	tr.Record(packet("NEW", 1, 47))
	if len(tr.TakeNotifications()) != 1 {
		t.Fatal("different farther station should trigger immediately")
	}
	*now = now.Add(31 * time.Minute)
	tr.Record(packet("NEW", 1, 47))
	if len(tr.TakeNotifications()) != 1 {
		t.Fatal("interval did not release tied current DX")
	}
	*now = now.Add(61 * time.Minute)
	tr.Snapshot()
	if len(tr.TakeNotifications()) != 0 {
		t.Fatal("expiration triggered RF notification")
	}
}

func TestStatusAndCapabilitiesAreDistinctPackets(t *testing.T) {
	tr, _ := testTracker()
	tr.Record(packet("FAR", 1, 45))
	status, err := tr.Report(1, "status", "Station operating", true)
	if err != nil || !strings.HasPrefix(status, ">Station operating DX=FAR(") {
		t.Fatalf("status: %q %v", status, err)
	}
	caps, err := tr.Report(1, "capabilities", "", true)
	if err != nil || !strings.HasPrefix(caps, "<IGATE,MSG_CNT=") || !strings.Contains(caps, ",DX=FAR(") {
		t.Fatalf("caps: %q %v", caps, err)
	}
	plain, _ := tr.Report(1, "capabilities", "", false)
	if strings.Contains(plain, "DX=") {
		t.Fatal(plain)
	}
}
