package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestDirectionFindingCreateListAndDelete(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"callsign":"f4aaa-9","mission":"SATER-01","latitude":42.9,"longitude":1.6,"bearing_deg":73.5,"uncertainty_deg":8,"frequency_mhz":121.5,"observed_at":"2026-10-22T14:30:00Z","note":"clear minimum"}`
	req := httptest.NewRequest(http.MethodPost, "/api/direction-findings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.DirectionFindingResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Callsign != "F4AAA-9" || created.BearingDeg != 73.5 {
		t.Fatalf("create mismatch: %+v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/direction-findings", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []dto.DirectionFindingResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/direction-findings/%d", created.ID), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDirectionFindingRejectsInvalidBearing(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{"callsign":"F4AAA-9","latitude":42.9,"longitude":1.6,"bearing_deg":360,"uncertainty_deg":8,"frequency_mhz":121.5,"observed_at":"2026-10-22T14:30:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/direction-findings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
