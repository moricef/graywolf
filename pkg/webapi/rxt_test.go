package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

type fakeRXTSource struct{ links []rxttelemetry.Link }

func (f fakeRXTSource) Enabled() bool                          { return true }
func (f fakeRXTSource) Endpoint() string                       { return "http://igate/rxt.json" }
func (f fakeRXTSource) SetEndpoint(string)                     {}
func (f fakeRXTSource) Snapshot(time.Time) []rxttelemetry.Link { return f.links }
func (f fakeRXTSource) Status(time.Time) rxttelemetry.Status {
	return rxttelemetry.Status{Enabled: true, Endpoint: f.Endpoint(), ActiveLinks: len(f.links)}
}

func TestRXTLinksResolvesStationPositions(t *testing.T) {
	mux := http.NewServeMux()
	cache := &mockStationCache{lookups: map[string]stationcache.LatLon{
		"F1AAA-1": {Lat: 42.1, Lon: 1.2},
		"F2BBB-2": {Lat: 43.1, Lon: 2.2},
	}}
	source := fakeRXTSource{links: []rxttelemetry.Link{{
		Hop:        rxttelemetry.Hop{From: "F1AAA-1", To: "F2BBB-2", HasData: true, RSSI: -112},
		ObservedAt: time.Now().UTC(),
	}}}
	RegisterRXT(nil, mux, source, cache, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rxt/links", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []RXTLinkDTO
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FromPosition == nil || got[0].ToPosition == nil {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got[0].FromPosition.Lat != 42.1 || got[0].ToPosition.Lon != 2.2 {
		t.Fatalf("unexpected positions: %+v", got[0])
	}
}

func TestRXTConfigCanBeUpdated(t *testing.T) {
	store, err := configstore.Open(filepath.Join(t.TempDir(), "graywolf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rxttelemetry.New("", nil, nil)
	mux := http.NewServeMux()
	RegisterRXT(nil, mux, source, &mockStationCache{}, store)

	req := httptest.NewRequest(http.MethodPut, "/api/rxt/config",
		bytes.NewBufferString(`{"endpoint":"http://192.168.1.161/rxt.json"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if source.Endpoint() != "http://192.168.1.161/rxt.json" {
		t.Fatalf("live endpoint=%q", source.Endpoint())
	}
	got, err := store.GetRXTConfig(req.Context())
	if err != nil || got.Endpoint != source.Endpoint() {
		t.Fatalf("stored config=%+v err=%v", got, err)
	}
}

func TestRXTStatusReportsSourceHealth(t *testing.T) {
	mux := http.NewServeMux()
	source := fakeRXTSource{links: []rxttelemetry.Link{{}}}
	RegisterRXT(nil, mux, source, &mockStationCache{}, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/rxt/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got rxttelemetry.Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Endpoint != source.Endpoint() || got.ActiveLinks != 1 {
		t.Fatalf("unexpected response: %+v", got)
	}
}
