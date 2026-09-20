package rxttelemetry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	rx := fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","schema_version":"1.0","event":"rx","event_id":"boot-1:13","boot_id":"boot-1","sequence":13,"receiver":{"station":"F4MLV-2"},"packet":{"raw_tnc2_base64":%q,"tnc2":%q,"source":{"text":"F6ZDD-10"},"path":[{"text":"F4MLV-10*","repeated":true},{"text":"WIDE2-1","repeated":false}]},"reception":{"local":{"rssi_dbm":-69,"snr_db":9.5,"frequency_error_hz":2062},"rxt":{"hops":[{"ordinal":1,"tx":"F6ZDD-10","rx":"F4MLV-10","has_data":true,"rssi_dbm":-116,"snr_db":4.75,"frequency_error_hz":-1782,"tth_ms":6791}]}}}`,
		base64.StdEncoding.EncodeToString(tnc2), string(tnc2))
	stream := hello + "\n" + rx + "\n"

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered []RXEvent
	s.SetPacketHandler(func(_ context.Context, event RXEvent) error {
		delivered = append(delivered, event)
		return nil
	})
	err := s.consumeStream(context.Background(), strings.NewReader(stream))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	if len(delivered) != 1 || string(delivered[0].TNC2) != string(tnc2) {
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

func TestConsumeStreamContinuesAfterMalformedPacketHandlerError(t *testing.T) {
	malformed := []byte("THIS IS BROKEN")
	valid := []byte("N0CALL>APRS:>ok")
	hello := `{"protocol":"lora-aprs-json","protocol_version":"1","event":"hello","boot_id":"boot-bad","latest_sequence":0}`
	rx := func(sequence int, payload []byte, status string) string {
		return fmt.Sprintf(`{"protocol":"lora-aprs-json","protocol_version":"1","event":"rx","event_id":"boot-bad:%d","boot_id":"boot-bad","sequence":%d,"receiver":{"station":"RX"},"packet":{"raw_tnc2_base64":%q,"parse_status":%q},"reception":{}}`,
			sequence, sequence, base64.StdEncoding.EncodeToString(payload), status)
	}
	stream := strings.Join([]string{
		hello,
		rx(1, malformed, "malformed"),
		rx(2, valid, "parsed"),
		"",
	}, "\n")

	s := New("http://igate.invalid/api/v1/aprs/stream", nil, nil)
	var delivered [][]byte
	s.SetPacketHandler(func(_ context.Context, event RXEvent) error {
		delivered = append(delivered, append([]byte(nil), event.TNC2...))
		if string(event.TNC2) == string(malformed) {
			return errors.New("not valid TNC2")
		}
		return nil
	})
	if err := s.consumeStream(context.Background(), strings.NewReader(stream)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("consumeStream error = %v, want unexpected EOF", err)
	}
	if len(delivered) != 2 || string(delivered[0]) != string(malformed) || string(delivered[1]) != string(valid) {
		t.Fatalf("delivered payloads = %q", delivered)
	}
	if got := s.Status(time.Now().UTC()).RecordsReceived; got != 2 {
		t.Fatalf("RecordsReceived = %d, want 2", got)
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
