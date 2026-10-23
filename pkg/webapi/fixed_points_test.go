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

func TestFixedPointCreateAndList(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"Aid Station 3","symbol_table":"/","symbol":"a","overlay":"","latitude":37.5,"longitude":-122.0}`
	req := httptest.NewRequest(http.MethodPost, "/api/fixed-points", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.FixedPointResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "Aid Station 3" || created.Latitude != 37.5 {
		t.Fatalf("create round-trip mismatch: %+v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/fixed-points", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list []dto.FixedPointResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", list)
	}
}

func TestFixedPointCreateEmptyNameReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"","latitude":1,"longitude":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/fixed-points", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestFixedPointDelete(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"X","latitude":1,"longitude":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/fixed-points", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var created dto.FixedPointResponse
	_ = json.NewDecoder(rec.Body).Decode(&created)

	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/fixed-points/%d", created.ID), nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestManualStationCreateAndReload(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{"name":" f4mlv-16 ","symbol_table":"/","symbol":"-","latitude":43,"longitude":1.3,"operator_station":true}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/fixed-points", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create station: %d: %s", rec.Code, rec.Body.String())
	}
	var station dto.FixedPointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &station); err != nil {
		t.Fatal(err)
	}
	if station.Name != "F4MLV-16" || !station.OperatorStation {
		t.Fatalf("station identity lost: %+v", station)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/fixed-points/%d", station.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("reload: %d: %s", rec.Code, rec.Body.String())
	}
	var reloaded dto.FixedPointResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &reloaded); err != nil {
		t.Fatal(err)
	}
	if !reloaded.OperatorStation || reloaded.Name != station.Name || reloaded.Latitude != 43 {
		t.Fatalf("station not persisted: %+v", reloaded)
	}
	// The persisted identity can be used as the source of a manual bearing.
	bearing := fmt.Sprintf(`{"callsign":%q,"latitude":%f,"longitude":%f,"bearing_deg":90,"frequency_mhz":121.5,"uncertainty_deg":10,"observed_at":"2026-10-23T10:00:00Z"}`,
		reloaded.Name, reloaded.Latitude, reloaded.Longitude)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/direction-findings", strings.NewReader(bearing)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("bearing from manual station: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestManualStationRejectsInvalidIdentity(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{"name":"not a callsign","latitude":43,"longitude":1.3,"operator_station":true}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/fixed-points", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
