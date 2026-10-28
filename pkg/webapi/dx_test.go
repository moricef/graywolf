package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDXConfigAPIReportsIndependentOfBeacons(t *testing.T) {
	s, _ := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	var applied rfdx.Config
	var kind string
	reset := false
	RegisterDX(s, mux, func(c rfdx.Config) { applied = c }, func(_ context.Context, k string) error { kind = k; return nil }, func(context.Context) error { reset = true; return nil })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/dx/config", nil))
	var cfg rfdx.Config
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &cfg) != nil || cfg.WindowMinutes != 60 || cfg.CapabilitiesIntervalMinutes != 60 || cfg.StatusEnabled || cfg.CapabilitiesEnabled {
		t.Fatalf("defaults: %s", w.Body.String())
	}
	cfg.Channel = 1
	cfg.StatusEnabled = true
	cfg.CapabilitiesEnabled = true
	cfg.SendPath = "is_only"
	cfg.StatusText = "test status"
	body, _ := json.Marshal(cfg)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/dx/config", bytes.NewReader(body)))
	if w.Code != 200 || applied != cfg {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	rows, _ := s.store.ListBeacons(context.Background())
	if len(rows) != 0 {
		t.Fatal("status configuration created a user beacon")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/dx/reports/capabilities", nil))
	if w.Code != 200 || kind != "capabilities" {
		t.Fatal(w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/dx/record", nil))
	if w.Code != 200 || !reset {
		t.Fatal(w.Body.String())
	}
	cfg.WindowMinutes = 0
	body, _ = json.Marshal(cfg)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/dx/config", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatal("invalid window accepted")
	}
}
