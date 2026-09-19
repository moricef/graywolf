package rxttelemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
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
