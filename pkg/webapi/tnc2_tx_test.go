package webapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTNC2TXAPIUsesBase64AndOnlyQueuesValidRequests(t *testing.T) {
	raw := append([]byte("F4MLV-GS>425WV3:"), 0x1d, 'w', '2', '6', 'l', 0x1c)
	var got []byte
	mux := http.NewServeMux()
	RegisterTNC2TX(nil, mux, func(_ context.Context, packet []byte) error {
		got = bytes.Clone(packet)
		return nil
	})
	body := `{"raw_tnc2_base64":"` + base64.StdEncoding.EncodeToString(raw) + `"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tnc2/tx", strings.NewReader(body)))
	if w.Code != http.StatusAccepted || !bytes.Equal(got, raw) {
		t.Fatalf("response=%d, bytes=%x", w.Code, got)
	}
	for _, bad := range []string{`{}`, `{"raw_tnc2_base64":"!!"}`} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tnc2/tx", strings.NewReader(bad)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid body %q: status %d", bad, w.Code)
		}
	}
	mux = http.NewServeMux()
	RegisterTNC2TX(nil, mux, func(context.Context, []byte) error { return errors.New("source refused") })
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tnc2/tx", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("policy refusal status %d", w.Code)
	}
}
