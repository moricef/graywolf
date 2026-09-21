package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/internal/testsync"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// TestAPRSJSONMicEEndToEnd exercises the complete versioned JSON receive path:
// NDJSON/base64 -> RawReception -> lossless TNC2 packet -> APRS Mic-E decode ->
// packet log and station map. The Mic-E longitude hundredths byte is 0x1c, so
// treating the authoritative packet as ordinary text would corrupt this case.
//
// The destination and information bytes follow APRS101 chapter 10:
// latitude 35°30.00'N, longitude 72°30.00'W, speed 123 kt, course 234°,
// car symbol ('/' table, '>' code).
func TestAPRSJSONMicEEndToEnd(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	info := []byte{
		'`',
		'd', ':', 0x1c, // longitude 72°30.00'W
		'(', '<', '>', // speed 123 kt, course 234°
		'>', '/', // symbol code and table
	}
	raw := append([]byte("F4JJE-16>35SP0P:"), info...)
	rawBefore := bytes.Clone(raw)
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"hello","boot_id":"mice-boot","uptime_ms":10,"latest_sequence":0,"producer":{"station":"N0RX-GS","software":"graywolf-test"},"capabilities":{"events":["rx"],"features":["local_metrics","rxt"],"transports":["ndjson-http"]}}`
	rx := fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"rx","event_id":"mice-boot:1","boot_id":"mice-boot","sequence":1,"created_at":"2026-09-21T10:00:00Z","uptime_ms":20,"receiver":{"station":"N0RX-GS","interface":"lora0"},"packet":{"raw_tnc2_base64":%q,"parse_status":"parsed","source":{"text":"F4JJE-16","call":"F4JJE","suffix":"16"},"destination":{"text":"35SP0P","call":"35SP0P"},"path":[],"information":{"raw_base64":%q}},"reception":{"crc_valid":true,"local":{"rssi_dbm":-69,"snr_db":9.5,"frequency_error_hz":2062},"radio":{"frequency_hz":433775000,"bandwidth_hz":125000,"spreading_factor":12,"coding_rate":"4/6"},"rxt":{"encoding":"rxt-v1","raw":")!AC","hops":[{"ordinal":1,"identity_status":"resolved","tx":"F4JJE-16","rx":"REMOTE-GS","has_data":true,"rssi_dbm":-122,"snr_db":-9,"frequency_error_hz":-208,"tth_ms":4158}]}}}`,
		base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(info))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, hello+"\n"+rx+"\n")
	}))
	defer server.Close()

	service := rxttelemetry.New(server.URL, server.Client(), quietLogger())
	service.SetPacketHandler(h.app.aprsJSONProduce)
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() {
		service.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("RXT service did not stop")
		}
	}()

	testsync.WaitFor(t, func() bool {
		return h.app.plog.Len() == 1 &&
			service.Status(time.Now().UTC()).RecordsReceived == 1 &&
			len(service.Snapshot(time.Now().UTC())) == 2
	}, 2*time.Second, "Mic-E JSON reception, local link, and RXT link")

	entries := h.app.plog.Query(packetlog.Filter{Channel: -1})
	if len(entries) != 1 {
		t.Fatalf("packet log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.APRSJSON == nil || entry.APRSJSON.Packet == nil {
		t.Fatalf("lossless reception missing from packet log: %+v", entry)
	}
	if !bytes.Equal(raw, rawBefore) ||
		!bytes.Equal(entry.APRSJSON.RawTNC2, rawBefore) ||
		!bytes.Equal(entry.APRSJSON.Packet.Raw, rawBefore) ||
		!bytes.Equal(entry.APRSJSON.Packet.Information, info) {
		t.Fatalf("Mic-E bytes changed: input=%x reception=%x packet=%x info=%x",
			rawBefore, entry.APRSJSON.RawTNC2, entry.APRSJSON.Packet.Raw,
			entry.APRSJSON.Packet.Information)
	}
	if !bytes.Equal(entry.APRSJSON.RawEvent, []byte(rx)) {
		t.Fatal("raw NDJSON rx record was not preserved exactly")
	}
	if entry.Decoded == nil || entry.Decoded.Type != aprs.PacketMicE || entry.Decoded.Position == nil {
		t.Fatalf("Mic-E was not decoded: %+v", entry.Decoded)
	}
	position := entry.Decoded.Position
	assertNear(t, "latitude", position.Latitude, 35.5, 0.0001)
	assertNear(t, "longitude", position.Longitude, -72.5, 0.0001)
	if position.Speed != 123 || !position.HasCourse || position.Course != 234 {
		t.Fatalf("motion = speed %.0f course %d has_course=%v", position.Speed, position.Course, position.HasCourse)
	}
	if position.Symbol.Table != '/' || position.Symbol.Code != '>' {
		t.Fatalf("symbol = %q%q, want '/''>'", position.Symbol.Table, position.Symbol.Code)
	}

	stations := h.app.stationCache.QueryBBox(stationcache.BBox{
		SwLat: -90, SwLon: -180, NeLat: 90, NeLon: 180,
	}, time.Hour)
	if len(stations) != 1 || stations[0].Callsign != "F4JJE-16" || len(stations[0].Positions) != 1 {
		t.Fatalf("mapped stations = %+v", stations)
	}
	mapped := stations[0].Positions[0]
	assertNear(t, "mapped latitude", mapped.Lat, 35.5, 0.0001)
	assertNear(t, "mapped longitude", mapped.Lon, -72.5, 0.0001)
	if mapped.Speed != 123 || !mapped.HasCourse || mapped.Course != 234 {
		t.Fatalf("mapped motion = %+v", mapped)
	}

	links := service.Snapshot(time.Now().UTC())
	byEndpoints := make(map[string]rxttelemetry.Link, len(links))
	for _, link := range links {
		byEndpoints[link.From+">"+link.To] = link
	}
	if link := byEndpoints["F4JJE-16>REMOTE-GS"]; link.RSSI != -122 || link.TTH != 4158 {
		t.Fatalf("RXT link = %+v", link)
	}
	if link := byEndpoints["F4JJE-16>N0RX-GS"]; link.RSSI != -69 || link.SNR != 9.5 || link.FO != 2062 {
		t.Fatalf("local link = %+v", link)
	}

	select {
	case packet := <-h.aprsOut:
		t.Fatalf("receive-only Mic-E JSON reached APRS output: %+v", packet)
	default:
	}
	if h.digiEmits.Len() != 0 {
		t.Fatal("receive-only Mic-E JSON reached RF/digipeater output")
	}
}

func assertNear(t *testing.T, field string, got, want, tolerance float64) {
	t.Helper()
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("%s = %.6f, want %.6f ± %.6f", field, got, want, tolerance)
	}
}
