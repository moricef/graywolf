package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

type fakeRXTSource struct{ links []rxttelemetry.Link }

func (f fakeRXTSource) Enabled() bool                          { return true }
func (f fakeRXTSource) Snapshot(time.Time) []rxttelemetry.Link { return f.links }

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
	RegisterRXT(nil, mux, source, cache)
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
