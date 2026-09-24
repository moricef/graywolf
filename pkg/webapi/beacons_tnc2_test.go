package webapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestExtendedBeaconOnlyOnConfiguredTextChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{"type":"position","channel":1,"callsign":"F4MLV-GS","destination":"APGRWO","path":"WIDE1-1","latitude":42.9,"longitude":1.2,"symbol_table":"/","symbol":">","interval":1800,"send_path":"rf","enabled":true}`
	post := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
		return rec
	}
	if rec := post(); rec.Code != http.StatusBadRequest {
		t.Fatalf("legacy channel accepted extended address: %d %s", rec.Code, rec.Body.String())
	}
	srv.SetBeaconTextRFEnabled(func(ch uint32) bool { return ch == 1 })
	if rec := post(); rec.Code != http.StatusCreated {
		t.Fatalf("text channel rejected extended beacon: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBeaconUpdateRevalidatesInheritedExtendedCallsign(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	row := &configstore.Beacon{Type: "position", Channel: 1, Callsign: "F4MLV-GS",
		Destination: "APGRWO", Latitude: 42.9, Longitude: 1.2}
	if err := srv.store.CreateBeacon(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	// Omitting callsign preserves the stored override. If the channel no
	// longer has textual TX, that preserved identity must still be checked.
	body := `{"type":"position","channel":1,"destination":"APGRWO","latitude":42.9,"longitude":1.2,"interval":1800}`
	url := "/api/beacons/" + fmt.Sprint(row.ID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, url, strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "callsign") {
		t.Fatalf("legacy update accepted stored extended source: %d %s", rec.Code, rec.Body.String())
	}
	srv.SetBeaconTextRFEnabled(func(ch uint32) bool { return ch == 1 })
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, url, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("text update rejected stored extended source: %d %s", rec.Code, rec.Body.String())
	}
}
