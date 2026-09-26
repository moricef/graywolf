package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

const testRemoteCommandSecret = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func newRemoteCommandTestServer(t *testing.T, send func(context.Context, messages.SendMessageRequest) (*configstore.Message, error)) (*Server, *http.ServeMux) {
	t.Helper()
	fake := &fakeMessagesSvc{sendFn: send}
	srv, mux, _ := newMessagesTestServer(t, fake)
	if err := srv.store.UpsertStationConfig(context.Background(), configstore.StationConfig{Callsign: "F4MLV-2"}); err != nil {
		t.Fatal(err)
	}
	service, err := remoteactions.NewService(remoteactions.ServiceConfig{DB: srv.store.DB()})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetRemoteActions(service)
	return srv, mux
}

func requestJSON(t *testing.T, mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func createTestCommandCredential(t *testing.T, mux *http.ServeMux) dto.RemoteCommandCredential {
	t.Helper()
	rec := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/command-credentials", map[string]any{
		"name": "F4MLV-15 test iGate", "target_call": "F4MLV-15", "secret_b64url": testRemoteCommandSecret,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), testRemoteCommandSecret) {
		t.Fatal("credential response leaked secret")
	}
	var out dto.RemoteCommandCredential
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRemoteCommandCredentialNeverReturnsSecret(t *testing.T) {
	_, mux := newRemoteCommandTestServer(t, func(context.Context, messages.SendMessageRequest) (*configstore.Message, error) {
		return nil, errors.New("unused")
	})
	created := createTestCommandCredential(t, mux)
	if created.TargetCall != "F4MLV-15" || created.LastCounter != "0" || created.KeyID != "A" {
		t.Fatalf("unexpected credential: %+v", created)
	}
	rec := requestJSON(t, mux, http.MethodGet, "/api/remote-actions/command-credentials", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), testRemoteCommandSecret) || strings.Contains(rec.Body.String(), "secret_b64url") {
		t.Fatal("list response exposed secret material")
	}
}

func TestSendRemoteCommandSignsAndUsesMessagesTransport(t *testing.T) {
	var captured messages.SendMessageRequest
	_, mux := newRemoteCommandTestServer(t, func(_ context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
		captured = req
		return &configstore.Message{
			ID: 42, Direction: "out", OurCall: req.OurCall, ToCall: req.To,
			ThreadKind: messages.ThreadKindDM, Text: req.Text, Channel: req.Channel,
		}, nil
	})
	createTestCommandCredential(t, mux)
	channel := uint32(1)
	rec := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/commands/F4MLV-15", dto.RemoteCommandSendRequest{
		Command: "TX=OFF", Channel: &channel,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send status=%d body=%s", rec.Code, rec.Body.String())
	}
	want, err := remoteactions.BuildRemoteCommandEnvelope(testRemoteCommandSecret, "F4MLV-2", "F4MLV-15", 1, "TX=OFF")
	if err != nil {
		t.Fatal(err)
	}
	if captured.OurCall != "F4MLV-2" || captured.To != "F4MLV-15" || captured.Channel != 1 || captured.Text != want {
		t.Fatalf("captured request: %+v want envelope %q", captured, want)
	}
	var out dto.RemoteCommandSendResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Counter != "1" || out.Message.ID != 42 {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestSendFailureStillConsumesCounter(t *testing.T) {
	fail := true
	_, mux := newRemoteCommandTestServer(t, func(_ context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
		if fail {
			return nil, errors.New("ambiguous transport failure")
		}
		return &configstore.Message{ID: 43, Direction: "out", OurCall: req.OurCall, ToCall: req.To, ThreadKind: messages.ThreadKindDM, Text: req.Text}, nil
	})
	createTestCommandCredential(t, mux)
	first := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/commands/F4MLV-15", map[string]any{"command": "COMMIT"})
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	fail = false
	second := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/commands/F4MLV-15", map[string]any{"command": "COMMIT"})
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var out dto.RemoteCommandSendResponse
	if err := json.NewDecoder(second.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Counter != "2" {
		t.Fatalf("counter reused after failed send: %s", out.Counter)
	}
}
