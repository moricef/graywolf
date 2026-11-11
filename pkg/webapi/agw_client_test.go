package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestAGWClientAPIPersistence(t *testing.T) {
	srv, _ := newTestServer(t)
	channel, err := srv.store.GetChannel(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	channel.InputDeviceID = nil
	channel.OutputDeviceID = 0
	if err := srv.store.UpdateChannel(context.Background(), channel); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := `{"type":"tcp-client","tcp_protocol":"agwpe","remote_host":"127.0.0.1","remote_port":8000,"agw_port":1,"channel":1,"enabled":false}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/kiss", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var saved dto.KissResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", fmt.Sprintf("/api/kiss/%d", saved.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.TCPProtocol != "agwpe" || saved.AgwPort != 1 || saved.RemotePort != 8000 || saved.Mode != "tnc" || saved.Enabled {
		t.Fatalf("settings not preserved: %+v", saved)
	}
	invalid := strings.Replace(body, `"agw_port":1`, `"agw_port":16`, 1)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/kiss", strings.NewReader(invalid)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid radio port: %d %s", rec.Code, rec.Body.String())
	}
}
