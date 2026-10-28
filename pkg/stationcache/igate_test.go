package stationcache

import (
	"github.com/chrissnell/graywolf/pkg/aprs"
	"testing"
	"time"
)

func TestIGateBeforeAndAfterPosition(t *testing.T) {
	c := NewMemCache(time.Hour)
	defer c.Close()
	now := time.Now().UTC()
	caps := &aprs.DecodedAPRSPacket{Source: "F4GCF-14", Timestamp: now, Caps: &aprs.Capabilities{Entries: map[string]string{"IGATE": "", "RF_CNT": "29", "DX": "1*F5ZQB-10(139km@136°)"}}}
	entries := ExtractEntry(caps, "igate-is", "IS", 0)
	if len(entries) != 1 || entries[0].IGate == nil {
		t.Fatal("missing capability entry")
	}
	c.Update(entries)
	entries[0].IGate.Entries["RF_CNT"] = "999"
	pos := CacheEntry{Key: "stn:F4GCF-14", Callsign: "F4GCF-14", HasPos: true, Lat: 43, Lon: 0.5, Symbol: [2]byte{'/', 'Z'}, Comment: "position comment", Timestamp: now.Add(time.Minute)}
	c.Update([]CacheEntry{pos})
	box := BBox{-90, -180, 90, 180}
	stations := c.QueryBBox(box, time.Hour)
	if len(stations) != 1 || stations[0].IGate.Entries["RF_CNT"] != "29" || !stations[0].IGate.ReceivedAt.Equal(now) {
		t.Fatalf("lost report: %+v", stations)
	}
	stations[0].IGate.Entries["RF_CNT"] = "888"
	// New position and a stale capability must not destroy the original report.
	c.Update([]CacheEntry{pos})
	caps.Timestamp = now.Add(-time.Minute)
	caps.Caps.Entries["RF_CNT"] = "1"
	c.Update(ExtractEntry(caps, "igate-is", "IS", 0))
	got := c.QueryBBox(box, time.Hour)[0]
	if got.IGate.Entries["RF_CNT"] != "29" || got.Comment != "position comment" || got.Symbol != pos.Symbol || len(got.Positions) != 1 {
		t.Fatalf("report/position corrupted: %+v", got)
	}
	// A fresh bulletin without DX replaces the old one completely.
	caps.Timestamp = now.Add(time.Minute)
	delete(caps.Caps.Entries, "DX")
	c.Update(ExtractEntry(caps, "igate-is", "IS", 0))
	if _, ok := c.QueryBBox(box, time.Hour)[0].IGate.Entries["DX"]; ok {
		t.Fatal("stale DX retained")
	}
}
