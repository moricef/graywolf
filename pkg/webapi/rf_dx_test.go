package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestDirectRFDXAvailableWithoutIgate(t *testing.T) {
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{Latitude: 43, Longitude: 1, Timestamp: time.Now()})
	tracker := rfdx.New(cache)
	tracker.Record(&aprs.DecodedAPRSPacket{Source: "F5ZQB-10", Channel: 2, Direction: aprs.DirectionRF, Position: &aprs.Position{Latitude: 44, Longitude: 1}})
	tracker.RecordMessageDownlink(2)
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	RegisterIgate(srv, mux, nil, nil)
	RegisterRFDX(srv, mux, tracker)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/rf/dx", nil))
	var snapshot rfdx.Snapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || len(snapshot.Channels) != 1 || snapshot.Channels[0].Callsign != "F5ZQB-10" {
		t.Fatalf("DX response: %d %s", w.Code, w.Body.String())
	}
	if len(snapshot.Statistics) != 1 || snapshot.Statistics[0].MessageCount != 1 || snapshot.Statistics[0].DirectCount != 1 || snapshot.HeardWindowSeconds != 1800 {
		t.Fatalf("statistics response: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/igate", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("test must exercise disabled iGate")
	}
}

func TestStatisticsBeaconCreateEnabledSelection(t *testing.T) {
	for _, test := range []struct {
		name, field string
		want        bool
	}{
		{"disabled", `,"enabled":false`, false},
		{"enabled", `,"enabled":true`, true},
		{"legacy-default", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			srv, _ := newTestServer(t)
			mux := http.NewServeMux()
			srv.RegisterRoutes(mux)
			body := `{"type":"custom","custom_info":">status","channel":1,"callsign":"F4MLV-2","destination":"APGRWO","send_path":"is_only","interval":3600` + test.field + `}`
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
			var saved dto.BeaconResponse
			if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Enabled != test.want || saved.EverySeconds != 3600 || saved.Channel != 1 {
				t.Fatalf("create %s: %d %s", test.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestDXPublicationChannelValidation(t *testing.T) {
	if err := (dto.BeaconRequest{Type: "dx", Channel: 1, SendPath: "is_only"}).Validate(); err == nil {
		t.Fatal("DX must no longer be created as a user beacon")
	}
	c := rfdx.DefaultConfig()
	c.StatusEnabled = true
	if err := c.Validate(); err == nil {
		t.Fatal("select a receive channel before enabling reports")
	}
	c.Channel = 2
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
