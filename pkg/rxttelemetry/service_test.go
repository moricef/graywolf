package rxttelemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPollDecodesAndKeepsNewestLink(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"rx_time":"16:15:01","age_ms":1000,"packet":"A>B:test","rxt_hops":[{"from":"a-1","to":"b-2","has_data":true,"rssi_dbm":-112,"snr_db":4.5,"fo_hz":-200,"tth_ms":6200}]}]`))
	}))
	defer srv.Close()
	s := New(srv.URL, srv.Client(), nil)
	s.pollOnce(context.Background())
	links := s.Snapshot(now.Add(time.Second))
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1", len(links))
	}
	if links[0].From != "A-1" || links[0].To != "B-2" {
		t.Fatalf("unexpected link: %+v", links[0])
	}
	if links[0].RSSI != -112 || links[0].TTH != 6200 {
		t.Fatalf("unexpected metrics: %+v", links[0])
	}
	status := s.Status(time.Now().UTC())
	if status.LastSuccessAt == nil || status.LastError != "" || status.RecordsReceived != 1 || status.ActiveLinks != 1 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestPollFailureIsVisibleInStatus(t *testing.T) {
	s := New("http://127.0.0.1:1/rxt.json", &http.Client{Timeout: 100 * time.Millisecond}, nil)
	s.pollOnce(context.Background())
	status := s.Status(time.Now().UTC())
	if status.LastAttemptAt == nil || status.LastSuccessAt != nil || status.LastError == "" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestSnapshotExpiresLinks(t *testing.T) {
	s := New("http://example.invalid/rxt.json", nil, nil)
	now := time.Now().UTC()
	s.links["A>B"] = Link{Hop: Hop{From: "A", To: "B", HasData: true}, ObservedAt: now.Add(-DefaultLinkTTL - time.Second)}
	if got := s.Snapshot(now); len(got) != 0 {
		t.Fatalf("got %d expired links", len(got))
	}
}

func TestConsumeStreamDeliversPacketAndBuildsRXTAndLocalLinks(t *testing.T) {
	tnc2 := []byte("F6ZDD-10>APLRG1,F4MLV-10*,WIDE2-1:!L84^@O(dt# test")
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"hello","boot_id":"boot-1","latest_sequence":12}`
	rx := fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"rx","event_id":"boot-1:13","boot_id":"boot-1","sequence":13,"receiver":{"station":"F4MLV-2"},"packet":{"raw_tnc2_base64":%q,"tnc2":%q,"source":{"text":"FALSE-HINT"},"path":[{"text":"FALSE-DIGI*","repeated":true}]},"reception":{"local":{"rssi_dbm":-69,"snr_db":9.5,"frequency_error_hz":2062},"rxt":{"hops":[{"ordinal":1,"tx":"F6ZDD-10","rx":"F4MLV-10","has_data":true,"rssi_dbm":-116,"snr_db":4.75,"frequency_error_hz":-1782,"tth_ms":6791}]}}}`,
		base64.StdEncoding.EncodeToString(tnc2), string(tnc2))
	stream := hello + "\n" + rx + "\n"

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered []RawReception
	s.SetPacketHandler(func(_ context.Context, event RawReception) error {
		delivered = append(delivered, event)
		return nil
	})
	err := s.consumeStream(context.Background(), strings.NewReader(stream))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	if len(delivered) != 1 || string(delivered[0].RawTNC2) != string(tnc2) {
		t.Fatalf("delivered = %+v", delivered)
	}
	links := s.Snapshot(time.Now().UTC())
	if len(links) != 2 {
		t.Fatalf("got %d links, want RXT + local links: %+v", len(links), links)
	}
	got := make(map[string]Link, len(links))
	for _, link := range links {
		got[link.From+">"+link.To] = link
	}
	if got["F6ZDD-10>F4MLV-10"].TTH != 6791 {
		t.Fatalf("RXT link = %+v", got["F6ZDD-10>F4MLV-10"])
	}
	if local := got["F4MLV-10>F4MLV-2"]; local.RSSI != -69 || local.FO != 2062 {
		t.Fatalf("local link = %+v", local)
	}

	// Replaying the same boot/sequence must not inject or count it twice.
	_ = s.consumeStream(context.Background(), strings.NewReader(stream))
	if len(delivered) != 1 || s.Status(time.Now().UTC()).RecordsReceived != 1 {
		t.Fatalf("duplicate was accepted: delivered=%d status=%+v", len(delivered), s.Status(time.Now().UTC()))
	}
}

func TestLocalLinkIgnoresRepeatedRoutingAlias(t *testing.T) {
	tnc2Raw := []byte("F4KOL-4>APLRG1,F1ZDB-10*,F4GCF-4*,WIDE2*:test")
	rx := fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"rx","event_id":"boot-alias:1","boot_id":"boot-alias","sequence":1,"receiver":{"station":"F1ZDB-10"},"packet":{"raw_tnc2_base64":%q,"parse_status":"parsed"},"reception":{"local":{"rssi_dbm":-122,"snr_db":-1.5,"frequency_error_hz":460}}}`,
		base64.StdEncoding.EncodeToString(tnc2Raw))

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	if err := s.consumeStream(context.Background(), strings.NewReader(rx+"\n")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	links := s.Snapshot(time.Now().UTC())
	if len(links) != 1 {
		t.Fatalf("got %d links, want one local link: %+v", len(links), links)
	}
	if got := links[0]; got.From != "F4GCF-4" || got.To != "F1ZDB-10" {
		t.Fatalf("local link = %s>%s, want F4GCF-4>F1ZDB-10", got.From, got.To)
	}
}

func TestOfficialProtocolConsumerVectors(t *testing.T) {
	fixture, err := os.Open("testdata/lora-aprs-json-v1-consumer-vectors.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered []RawReception
	s.SetPacketHandler(func(_ context.Context, reception RawReception) error {
		delivered = append(delivered, reception)
		return nil
	})
	if err := s.consumeStream(context.Background(), fixture); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	if len(delivered) != 2 {
		t.Fatalf("delivered %d receptions, want 2", len(delivered))
	}
	if delivered[0].Packet == nil || delivered[0].Packet.Source.Text != "NN7LE-GS" || delivered[0].Packet.Source.Suffix != "GS" {
		t.Fatalf("official opaque suffix vector = %+v", delivered[0])
	}
	if delivered[1].Packet == nil || string(delivered[1].Packet.Information) != "keyboard-to-keyboard" || delivered[1].RXT == nil {
		t.Fatalf("official non-APRS/RXT vector = %+v", delivered[1])
	}
}

func TestConsumeStreamAcceptsLegacyHopWithoutInventingMetrics(t *testing.T) {
	tnc2 := []byte("F1SRC>APRS,F2LEG-1*,F3RXT-2*:>mixed")
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","event":"hello","boot_id":"boot-mixed","latest_sequence":0}`
	rx := fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","event":"rx","event_id":"boot-mixed:1","boot_id":"boot-mixed","sequence":1,"receiver":{"station":"F4RX"},"packet":{"raw_tnc2_base64":%q,"tnc2":%q,"source":{"text":"F1SRC"},"path":[{"text":"F2LEG-1*","repeated":true},{"text":"F3RXT-2*","repeated":true}]},"reception":{"rxt":{"hops":[{"ordinal":1,"tx":"F1SRC","rx":"F2LEG-1","has_data":false},{"ordinal":2,"tx":"F2LEG-1","rx":"F3RXT-2","has_data":true,"rssi_dbm":-116,"snr_db":4.75,"frequency_error_hz":-1782,"tth_ms":6791}]}}}`,
		base64.StdEncoding.EncodeToString(tnc2), string(tnc2))

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	if err := s.consumeStream(context.Background(), strings.NewReader(hello+"\n"+rx+"\n")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	links := s.Snapshot(time.Now().UTC())
	if len(links) != 1 {
		t.Fatalf("got %d links, want only the measured RXT link: %+v", len(links), links)
	}
	if links[0].From != "F2LEG-1" || links[0].To != "F3RXT-2" || !links[0].HasData {
		t.Fatalf("measured link = %+v", links[0])
	}
}

func TestConsumeStreamPreservesMalformedThenContinues(t *testing.T) {
	malformed := []byte("THIS IS BROKEN")
	valid := []byte("N0CALL>APRS:>ok")
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","event":"hello","boot_id":"boot-bad","latest_sequence":0}`
	rx := func(sequence int, payload []byte, status string) string {
		reception := `{}`
		if status == "malformed" {
			reception = `{"radio":{"bandwidth_hz":125000,"spreading_factor":12},"rxt":{"encoding":"rxt-v1","raw":")!AC","hops":[{"ordinal":1,"identity_status":"resolved","tx":"BROKEN-TX","rx":"REMOTE-RX","has_data":true,"rssi_dbm":-122,"snr_db":-9,"frequency_error_hz":-208,"tth_ms":4158}]}}`
		}
		return fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","event":"rx","event_id":"boot-bad:%d","boot_id":"boot-bad","sequence":%d,"receiver":{"station":"RX"},"packet":{"raw_tnc2_base64":%q,"parse_status":%q},"reception":%s}`,
			sequence, sequence, base64.StdEncoding.EncodeToString(payload), status, reception)
	}
	stream := strings.Join([]string{
		hello,
		rx(1, malformed, "malformed"),
		rx(2, valid, "parsed"),
		"",
	}, "\n")

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered []RawReception
	s.SetPacketHandler(func(_ context.Context, event RawReception) error {
		delivered = append(delivered, event)
		return nil
	})
	if err := s.consumeStream(context.Background(), strings.NewReader(stream)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	if len(delivered) != 2 || string(delivered[0].RawTNC2) != string(malformed) || string(delivered[1].RawTNC2) != string(valid) {
		t.Fatalf("delivered payloads = %+v", delivered)
	}
	if delivered[0].Packet != nil || delivered[1].Packet == nil {
		t.Fatalf("optional packets = malformed:%#v parsed:%#v", delivered[0].Packet, delivered[1].Packet)
	}
	if len(delivered[0].RawEvent) == 0 {
		t.Fatal("malformed reception did not preserve its raw JSON record")
	}
	if delivered[0].RXT == nil || len(delivered[0].RXT.Hops) != 1 {
		t.Fatalf("malformed reception lost RXT metadata: %+v", delivered[0].RXT)
	}
	if got := s.Status(time.Now().UTC()).RecordsReceived; got != 2 {
		t.Fatalf("RecordsReceived = %d, want 2", got)
	}
}

func TestHandlerFailureDoesNotAdvanceResumeCursor(t *testing.T) {
	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	s.SetResumeState("boot-1:4", "boot-1", true)
	s.lastSequence = 4
	s.SetPacketHandler(func(context.Context, RawReception) error {
		return errors.New("storage unavailable")
	})
	event := streamRecord{
		Protocol: "lora-aprs-json", ProtocolVersion: "1", SchemaVersion: "1.0",
		Event: "rx", EventID: "boot-1:5", BootID: "boot-1", Sequence: 5,
	}
	event.Packet.RawTNC2 = []byte("N0CALL>APRS:>not stored")
	event.Packet.ParseStatus = "parsed"
	if err := s.processStreamRecord(context.Background(), event); err == nil {
		t.Fatal("handler failure was ignored")
	}
	status := s.Status(time.Now().UTC())
	if status.LastEventID != "boot-1:4" || status.RecordsReceived != 0 {
		t.Fatalf("cursor advanced after preservation failure: %+v", status)
	}
}

func TestParsedClaimThatCannotBeDerivedIsStillPreserved(t *testing.T) {
	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered RawReception
	s.SetPacketHandler(func(_ context.Context, reception RawReception) error {
		delivered = reception
		return nil
	})
	event := streamRecord{
		Protocol: "lora-aprs-json", ProtocolVersion: "1", SchemaVersion: "1.0",
		Event: "rx", EventID: "boot-inconsistent:1", BootID: "boot-inconsistent", Sequence: 1,
	}
	event.Packet.RawTNC2 = []byte("not a TNC2 envelope")
	event.Packet.TNC2 = "a stale non-authoritative rendering"
	event.Packet.ParseStatus = "parsed"
	event.Packet.Source.Text = "FALSE-HINT"
	event.Packet.Path = []streamAddress{{Text: "FALSE-DIGI*", Repeated: true}}
	event.Receiver.Station = "LOCAL-RX"
	event.Reception.Local = &LocalMetrics{RSSI: -70, SNR: 8, FO: 123}
	rssi, snr, fo, tth := -120.0, -7.5, -200, 4000
	event.Reception.RXT = &RXTTelemetry{Hops: []RXTHop{{
		Ordinal: 1, TX: "RXT-TX", RX: "RXT-RX", IdentityStatus: "resolved", HasData: true,
		RSSI: &rssi, SNR: &snr, FO: &fo, TTH: &tth,
	}}}
	if err := s.processStreamRecord(context.Background(), event); err != nil {
		t.Fatalf("schema-level reception was discarded: %v", err)
	}
	if delivered.Packet != nil || len(delivered.Warnings) != 2 || string(delivered.RawTNC2) != "not a TNC2 envelope" {
		t.Fatalf("preserved inconsistent reception = %+v", delivered)
	}
	if status := s.Status(time.Now().UTC()); status.LastEventID != event.EventID {
		t.Fatalf("accepted cursor = %+v", status)
	}
	links := s.Snapshot(time.Now().UTC())
	if len(links) != 1 || links[0].From != "RXT-TX" || links[0].To != "RXT-RX" {
		t.Fatalf("derived hints created a local link or RXT was lost: %+v", links)
	}
	if links[0].Packet != string(event.Packet.RawTNC2) {
		t.Fatalf("link packet = %q, want authoritative raw %q", links[0].Packet, event.Packet.RawTNC2)
	}
}

func TestResumeLifecycle(t *testing.T) {
	s := New("http://igate.invalid/api/v1/aprs/stream?source=rf", nil, nil)
	var states []ResumeState
	s.SetResumeHandler(func(state ResumeState) error {
		states = append(states, state)
		return nil
	})

	hello := streamRecord{Protocol: "lora-aprs-json", ProtocolVersion: "1", Event: "hello", BootID: "boot-1"}
	hello.Capabilities.Features = []string{"local_metrics", "history_resume"}
	if err := s.processStreamRecord(context.Background(), hello); err != nil {
		t.Fatal(err)
	}
	rx := streamRecord{
		Protocol: "lora-aprs-json", ProtocolVersion: "1", Event: "rx",
		EventID: "boot-1:7", BootID: "boot-1", Sequence: 7,
	}
	rx.Packet.RawTNC2 = []byte("N0CALL>APRS:>resume")
	if err := s.processStreamRecord(context.Background(), rx); err != nil {
		t.Fatal(err)
	}

	gotURL, err := s.resumeURL(s.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "http://igate.invalid/api/v1/aprs/stream?after=boot-1%3A7&source=rf" {
		t.Fatalf("resume URL = %q", gotURL)
	}
	status := s.Status(time.Now().UTC())
	if !status.ResumeSupported || status.LastEventID != "boot-1:7" {
		t.Fatalf("status after rx = %+v", status)
	}
	if len(states) != 2 || states[1].EventID != "boot-1:7" || !states[1].Supported {
		t.Fatalf("persisted states = %+v", states)
	}

	gap := streamRecord{
		Protocol: "lora-aprs-json", ProtocolVersion: "1", Event: "gap",
		BootID: "boot-1", RequestedAfter: "boot-1:7", Reason: "history_expired",
	}
	if err := s.processStreamRecord(context.Background(), gap); err != nil {
		t.Fatal(err)
	}
	gotURL, err = s.resumeURL(s.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != s.Endpoint() {
		t.Fatalf("URL after gap = %q, want %q", gotURL, s.Endpoint())
	}
	if states[len(states)-1].EventID != "" {
		t.Fatalf("state after gap = %+v", states[len(states)-1])
	}
}

func TestHelloClearsCursorOnBootChangeOrCapabilityRemoval(t *testing.T) {
	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	s.SetResumeState("old-boot:9", "old-boot", true)

	hello := streamRecord{Protocol: "lora-aprs-json", ProtocolVersion: "1", Event: "hello", BootID: "new-boot"}
	hello.Capabilities.Features = []string{"history_resume"}
	if err := s.processStreamRecord(context.Background(), hello); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(time.Now().UTC()); status.LastEventID != "" || !status.ResumeSupported {
		t.Fatalf("status after boot change = %+v", status)
	}

	s.SetResumeState("new-boot:3", "new-boot", true)
	hello.Capabilities.Features = nil
	if err := s.processStreamRecord(context.Background(), hello); err != nil {
		t.Fatal(err)
	}
	if status := s.Status(time.Now().UTC()); status.LastEventID != "" || status.ResumeSupported {
		t.Fatalf("status without resume capability = %+v", status)
	}
}

func TestSequenceGapReconnectsFromLastCursor(t *testing.T) {
	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	s.SetResumeState("boot-1:4", "boot-1", true)
	s.lastSequence = 4

	event := streamRecord{
		Protocol: "lora-aprs-json", ProtocolVersion: "1", Event: "rx",
		EventID: "boot-1:6", BootID: "boot-1", Sequence: 6,
	}
	event.Packet.RawTNC2 = []byte("N0CALL>APRS:>gap")
	err := s.processStreamRecord(context.Background(), event)
	if err == nil || !strings.Contains(err.Error(), "sequence gap") {
		t.Fatalf("processStreamRecord error = %v", err)
	}
	if status := s.Status(time.Now().UTC()); status.LastEventID != "boot-1:4" || status.RecordsReceived != 0 {
		t.Fatalf("status after sequence gap = %+v", status)
	}
}

func TestUnsupportedResumeResponseClearsPersistedCursor(t *testing.T) {
	var requestedURL string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":"history_resume_unsupported"}`)),
			Request:    req,
		}, nil
	})}
	s := New("http://igate.invalid/api/v1/aprs/stream", client, nil)
	s.SetResumeState("boot-1:9", "boot-1", true)
	var persisted ResumeState
	s.SetResumeHandler(func(state ResumeState) error {
		persisted = state
		return nil
	})

	if _, err := s.consumeOnce(context.Background(), s.Endpoint()); err == nil {
		t.Fatal("consumeOnce succeeded, want HTTP 400")
	}
	if requestedURL != "http://igate.invalid/api/v1/aprs/stream?after=boot-1%3A9" {
		t.Fatalf("requested URL = %q", requestedURL)
	}
	if persisted.EventID != "" || persisted.BootID != "" || persisted.Supported {
		t.Fatalf("persisted state after HTTP 400 = %+v", persisted)
	}
	if got, err := s.resumeURL(s.Endpoint()); err != nil || got != s.Endpoint() {
		t.Fatalf("URL after HTTP 400 = %q, err=%v", got, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRunStartsAfterRuntimeConfigurationAndReconnects(t *testing.T) {
	calls := make(chan struct{}, 4)
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"hello","boot_id":"boot-1","latest_sequence":0}` + "\n"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if accept := req.Header.Get("Accept"); !strings.Contains(accept, "application/x-ndjson") {
			t.Errorf("Accept = %q", accept)
		}
		calls <- struct{}{}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/x-ndjson"}},
			Body:       io.NopCloser(strings.NewReader(hello)),
			Request:    req,
		}, nil
	})}
	s := New("", client, nil)
	s.retry = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	s.SetEndpoint("http://igate.invalid/api/v1/aprs/stream")
	for attempt := 1; attempt <= 2; attempt++ {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for connection attempt %d", attempt)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}
