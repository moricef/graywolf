package app

import (
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"testing"
	"time"
)

func TestJSONIngressUpdatesEachReceiverDXWithoutTX(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()
	now := time.Now().UTC()
	h.app.stationCache.Update([]stationcache.CacheEntry{{Key: "stn:F4MLV-10", Callsign: "F4MLV-10", HasPos: true, Lat: 43, Lon: 1, Timestamp: now}, {Key: "stn:F1ZDB-13", Callsign: "F1ZDB-13", HasPos: true, Lat: 44, Lon: 1, Timestamp: now}})
	h.app.receiverDX = stationcache.NewReceiverTracker(h.app.stationCache)
	raw := []byte("ORIGIN>APRS,F4MLV-10*,WIDE2-1:!4500.00N/00100.00E#test")
	packet, err := tnc2.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	event := rxttelemetry.RawReception{RawTNC2: raw, Packet: packet, Receiver: "F1ZDB-13", CRCValid: true, Local: &rxttelemetry.LocalMetrics{RSSI: -90, SNR: 4}, RXT: &rxttelemetry.RXTTelemetry{Hops: []rxttelemetry.RXTHop{{TX: "ORIGIN", RX: "F4MLV-10", HasData: true}}}}
	if err = h.app.aprsJSONProduce(h.ctx, event); err != nil {
		t.Fatal(err)
	}
	got := h.app.receiverDX.ReceiverSnapshot(time.Now())
	if got["F4MLV-10"].DX == nil || got["F4MLV-10"].DX.Callsign != "ORIGIN" || got["F4MLV-10"].DX.DistanceKM < 220 {
		t.Fatalf("origin radio link missing: %+v", got)
	}
	if got["F1ZDB-13"].DX == nil || got["F1ZDB-13"].DX.Callsign != "F4MLV-10" || got["F1ZDB-13"].DX.DistanceKM > 113 {
		t.Fatalf("relay origin incorrectly assigned: %+v", got)
	}
	select {
	case <-h.aprsOut:
		t.Fatal("remote DX caused APRS output")
	default:
	}
	if h.digiEmits.Len() != 0 {
		t.Fatal("remote DX caused RF output")
	}
}
