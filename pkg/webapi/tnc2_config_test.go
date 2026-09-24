package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestTNC2ConfigAPIUpdatesLiveSettings(t *testing.T) {
	mux := http.NewServeMux()
	current := TNC2ConfigStatus{TNC2Config: configstore.TNC2Config{SerialBaud: 115200, TXChannel: 1, MaxTXBytes: 255}}
	RegisterTNC2Config(mux, func() TNC2ConfigStatus { return current }, func(_ context.Context, cfg configstore.TNC2Config) error {
		current.TNC2Config = cfg
		return nil
	})
	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/tnc2/config", bytes.NewBufferString(body)))
		return rec
	}
	if rec := put(`{"serial_device":"/dev/ttyUSB0","serial_baud":115200,"tx_transport":"serial","tx_source":"F4MLV-GS","tx_channel":1,"max_tx_bytes":255}`); rec.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", rec.Code, rec.Body.String())
	}
	if current.TXTransport != "serial" || current.TXSource != "F4MLV-GS" {
		t.Fatalf("live config=%+v", current)
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/tnc2/config", nil))
	var got TNC2ConfigStatus
	if err := json.NewDecoder(get.Body).Decode(&got); err != nil || got.SerialDevice != "/dev/ttyUSB0" {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if rec := put(`{"tx_transport":"serial","tx_source":"F4MLV-GS","tx_channel":1,"max_tx_bytes":255}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid config status=%d", rec.Code)
	}
	if current.TXTransport != "serial" {
		t.Fatal("invalid save changed live config")
	}
	if rec := put(`{"tcp_connected":true}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("read-only status accepted: %d", rec.Code)
	}
}
