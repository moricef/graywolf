package stationcache

import (
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"math"
	"testing"
	"time"
)

func TestReceiverDXIndependentPositionsAndExpiry(t *testing.T) {
	cache := NewMemCache(time.Hour)
	defer cache.Close()
	now := time.Now().UTC()
	position := func(call string, lat float64) {
		cache.Update([]CacheEntry{{Key: "stn:" + call, Callsign: call, HasPos: true, Lat: lat, Lon: 1, Timestamp: now}})
	}
	position("F4MLV-10", 43)
	position("F1ZDB-13", 44)
	position("MOBILE", 45)
	tracker := NewReceiverTracker(cache)
	cfg := rfdx.DefaultConfig()
	cfg.MinDistanceKM = 30
	tracker.Configure(cfg)
	link := func(rx string, at time.Time) rxttelemetry.Link {
		return rxttelemetry.Link{Hop: rxttelemetry.Hop{From: "MOBILE", To: rx, HasData: true, RSSI: -100, SNR: 0, FO: 4}, ObservedAt: at}
	}
	tracker.Record([]rxttelemetry.Link{link("F4MLV-10", now), link("F1ZDB-13", now)}, now)
	got := tracker.ReceiverSnapshot(now)
	if math.Abs(got["F4MLV-10"].DX.DistanceKM-222.4) > 1 || math.Abs(got["F1ZDB-13"].DX.DistanceKM-111.2) > 1 {
		t.Fatalf("wrong receiver origins: %+v", got)
	}
	// Moving closer must retain the previous farther reception until it expires.
	position("MOBILE", 43.5)
	tracker.Record([]rxttelemetry.Link{link("F4MLV-10", now.Add(30*time.Minute))}, now.Add(30*time.Minute))
	if tracker.ReceiverSnapshot(now.Add(31 * time.Minute))["F4MLV-10"].DX.DistanceKM < 220 {
		t.Fatal("lost farther reception")
	}
	got = tracker.ReceiverSnapshot(now.Add(61 * time.Minute))
	if got["F4MLV-10"].DX == nil || got["F4MLV-10"].DX.DistanceKM > 57 {
		t.Fatalf("wrong expiry: %+v", got)
	}
	if _, ok := got["F1ZDB-13"]; ok {
		t.Fatal("expired receiver remains")
	}
	cfg.IgnoredCalls = "MOBILE"
	tracker.Configure(cfg)
	got = tracker.ReceiverSnapshot(now.Add(61 * time.Minute))
	if got["F4MLV-10"].DX != nil {
		t.Fatal("ignore filter not applied")
	}
}

func TestReceiverDXPendingPositionsAndMeasuredOnly(t *testing.T) {
	cache := NewMemCache(time.Hour)
	defer cache.Close()
	tracker := NewReceiverTracker(cache)
	now := time.Now().UTC()
	measured := rxttelemetry.Link{Hop: rxttelemetry.Hop{From: "F4JJE-16", To: "F4MLV-10", HasData: true}, ObservedAt: now}
	tracker.Record([]rxttelemetry.Link{measured, {Hop: rxttelemetry.Hop{From: "GHOST", To: "F4MLV-10", HasData: false}, ObservedAt: now}}, now)
	got := tracker.ReceiverSnapshot(now)["F4MLV-10"]
	if got.DX != nil || got.DirectStations != 1 {
		t.Fatalf("invented position/measurement: %+v", got)
	}
	cache.Update([]CacheEntry{{Key: "stn:F4JJE-16", Callsign: "F4JJE-16", HasPos: true, Lat: 44, Lon: 1, Timestamp: now}, {Key: "stn:F4MLV-10", Callsign: "F4MLV-10", HasPos: true, Lat: 43, Lon: 1, Timestamp: now}})
	got = tracker.ReceiverSnapshot(now)["F4MLV-10"]
	if got.DX == nil || got.DX.Callsign != "F4JJE-16" {
		t.Fatalf("pending identity not resolved: %+v", got)
	}
	got.DX.Callsign = "CHANGED"
	if tracker.ReceiverSnapshot(now)["F4MLV-10"].DX.Callsign != "F4JJE-16" {
		t.Fatal("snapshot mutated tracker")
	}
}
