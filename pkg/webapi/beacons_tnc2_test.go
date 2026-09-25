package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestCustomBeaconFieldsRoundTripOnConfiguredTextChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	srv.SetBeaconTextRFEnabled(func(ch uint32) bool { return ch == 1 })

	body := `{
		"type":"custom",
		"channel":1,
		"callsign":"F4MLV-GS",
		"destination":"APGRWO",
		"path":"WIDE1-1",
		"custom_info":">weather:",
		"comment":"static",
		"comment_cmd":"/usr/local/bin/weather-comment --short",
		"interval":600,
		"send_path":"rf",
		"enabled":true
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create custom text beacon: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Type != "custom" || created.CustomInfo != ">weather:" ||
		created.Comment != "static" || created.CommentCmd != "/usr/local/bin/weather-comment --short" {
		t.Fatalf("custom fields lost on create: %+v", created)
	}

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/beacons/"+fmt.Sprint(created.ID), nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get custom text beacon: %d %s", get.Code, get.Body.String())
	}
	var fetched dto.BeaconResponse
	if err := json.NewDecoder(get.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.CustomInfo != created.CustomInfo || fetched.CommentCmd != created.CommentCmd {
		t.Fatalf("custom fields lost on GET: created=%+v fetched=%+v", created, fetched)
	}
}

func TestWeatherBeaconFieldsRoundTripOnConfiguredTextChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	srv.SetBeaconTextRFEnabled(func(ch uint32) bool { return ch == 1 })

	body := `{
		"type":"weather", "channel":1, "callsign":"F4JJE-16",
		"destination":"APGRWO", "path":"NN7LE-GS", "use_gps":true,
		"weather_source":"wxnow_file", "weather_path":"/var/lib/weather/WxNow.txt",
		"interval":600, "send_path":"rf", "enabled":true
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create weather text beacon: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Type != "weather" || created.WeatherSource != "wxnow_file" ||
		created.WeatherPath != "/var/lib/weather/WxNow.txt" {
		t.Fatalf("weather fields lost on create: %+v", created)
	}
}

func TestDavisWeatherFieldsRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{
		"type":"weather", "channel":0, "callsign":"F4JJE-15",
		"destination":"APGRWO", "use_gps":true,
		"weather_source":"davis_serial", "weather_device":"/dev/ttyUSB0",
		"weather_baud":19200, "weather_bucket":"0.2mm",
		"interval":600, "send_path":"is_only", "enabled":true
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create Davis weather beacon: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.WeatherSource != "davis_serial" || created.WeatherDevice != "/dev/ttyUSB0" ||
		created.WeatherBaud != 19200 || created.WeatherBucket != "0.2mm" {
		t.Fatalf("Davis fields lost: %+v", created)
	}
}

func TestPeetWeatherFieldsRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{
		"type":"weather", "channel":0, "callsign":"F4JJE-13",
		"destination":"APGRWO", "use_gps":true,
		"weather_source":"peet_serial", "weather_device":"/dev/ttyUSB1",
		"weather_baud":2400, "weather_bucket":"0.1mm",
		"interval":600, "send_path":"is_only", "enabled":true
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create Peet Bros weather beacon: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.WeatherSource != "peet_serial" || created.WeatherDevice != "/dev/ttyUSB1" ||
		created.WeatherBaud != 2400 || created.WeatherBucket != "0.1mm" {
		t.Fatalf("Peet Bros fields lost: %+v", created)
	}
}

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
