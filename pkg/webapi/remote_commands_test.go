package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	if created.TargetCall != "F4MLV-15" || created.SignedTarget != "F4MLV-15" || created.LastCounter != "0" || created.KeyID != "A" {
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

func TestTacticalRemoteCommandSignsIGateIdentityAndAddressesRFName(t *testing.T) {
	var captured messages.SendMessageRequest
	srv, mux := newRemoteCommandTestServer(t, func(_ context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
		captured = req
		return &configstore.Message{ID: 44, Direction: "out", OurCall: req.OurCall, ToCall: req.To, ThreadKind: messages.ThreadKindDM, Text: req.Text}, nil
	})
	rec := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/command-credentials", map[string]any{
		"name": "tactical", "target_call": "burntx", "signed_target": " f4mlv-10 ", "secret_b64url": testRemoteCommandSecret,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.RemoteCommandCredential
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.TargetCall != "BURNTX" || created.SignedTarget != "F4MLV-10" {
		t.Fatalf("credential: %+v", created)
	}
	if err := srv.store.DB().Exec("UPDATE remote_command_credentials SET last_counter=70 WHERE id=?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	rec = requestJSON(t, mux, http.MethodPost, "/api/remote-actions/commands/BURNTX", map[string]any{"command": "TX=OFF"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	if captured.To != "BURNTX" || captured.OurCall != "F4MLV-2" || captured.Text != "!RC1:A:1Z:TX=OFF:OLkaJxyKIXBM9SrF" {
		t.Fatalf("incorrect RF address or HMAC: %+v", captured)
	}
	path := "/api/remote-actions/command-credentials/" + strconv.Itoa(int(created.ID))
	for _, identity := range []string{"n7uv", ""} {
		rec = requestJSON(t, mux, http.MethodPut, path, map[string]any{"signed_target": identity})
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
		var updated dto.RemoteCommandCredential
		if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
			t.Fatal(err)
		}
		want := strings.ToUpper(identity)
		if want == "" {
			want = "BURNTX"
		}
		if updated.SignedTarget != want || updated.LastCounter != "71" || updated.LastUsedAt == nil {
			t.Fatalf("identity update changed counter: %+v", updated)
		}
	}
	rec = requestJSON(t, mux, http.MethodPut, path, map[string]any{"signed_target": "F4MLV-100"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid identity accepted: %d", rec.Code)
	}
	rec = requestJSON(t, mux, http.MethodGet, "/api/remote-actions/command-credentials", nil)
	var listed []dto.RemoteCommandCredential
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(listed) != 1 || listed[0].SignedTarget != "BURNTX" || strings.Contains(rec.Body.String(), "secret_b64url") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
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

func TestMoveRemoteCommandCredentialPreservesKeyAndCounter(t *testing.T) {
	srv, mux := newRemoteCommandTestServer(t, func(context.Context, messages.SendMessageRequest) (*configstore.Message, error) {
		return nil, errors.New("no transmission expected")
	})
	created := createTestCommandCredential(t, mux)
	if err := srv.store.DB().Exec("UPDATE remote_command_credentials SET last_counter=71 WHERE id=?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/remote-actions/command-credentials/" + strconv.Itoa(int(created.ID))
	rec := requestJSON(t, mux, http.MethodPut, path, map[string]any{
		"target_call": "genatx", "signed_target": "F4MLV-15",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	row, err := srv.remoteActions.Commands().Get(context.Background(), created.ID)
	if err != nil || row.TargetCall != "GENATX" || row.SignedTarget != "F4MLV-15" || row.LastCounter != 71 || row.SecretBase64URL != testRemoteCommandSecret {
		t.Fatalf("move changed key/counter: %+v, %v", row, err)
	}
	if _, err := srv.remoteActions.Commands().GetByTarget(context.Background(), "F4MLV-15"); err == nil {
		t.Fatal("old lookup still resolves moved credential")
	}
	createTestCommandCredential(t, mux) // Another key now occupies the original recipient.
	rec = requestJSON(t, mux, http.MethodPut, path, map[string]any{"target_call": "F4MLV-15", "signed_target": "F4MLV-15"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate recipient: %d %s", rec.Code, rec.Body.String())
	}
	row, err = srv.remoteActions.Commands().Get(context.Background(), created.ID)
	if err != nil || row.TargetCall != "GENATX" || row.LastCounter != 71 || row.SecretBase64URL != testRemoteCommandSecret {
		t.Fatalf("collision modified key: %+v, %v", row, err)
	}
}

func TestRemoteCommandUpdateOmittedIdentityPreservesSignature(t *testing.T) {
	var captured messages.SendMessageRequest
	srv, mux := newRemoteCommandTestServer(t, func(_ context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
		captured = req
		return &configstore.Message{ID: 45, Direction: "out", OurCall: req.OurCall, ToCall: req.To, ThreadKind: messages.ThreadKindDM, Text: req.Text}, nil
	})
	rec := requestJSON(t, mux, http.MethodPost, "/api/remote-actions/command-credentials", map[string]any{
		"name": "tactical", "target_call": "BURNTX", "signed_target": "F4MLV-10", "secret_b64url": testRemoteCommandSecret,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.RemoteCommandCredential
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.DB().Exec("UPDATE remote_command_credentials SET last_counter=70 WHERE id=?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/remote-actions/command-credentials/" + strconv.Itoa(int(created.ID))
	for _, body := range []map[string]any{
		{"name": "renamed"},
		{"name": "renamed again", "signed_target": nil},
	} {
		rec = requestJSON(t, mux, http.MethodPut, path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
		}
		row, err := srv.remoteActions.Commands().Get(context.Background(), created.ID)
		if err != nil || row.SignedTarget != "F4MLV-10" || row.LastCounter != 70 || row.SecretBase64URL != testRemoteCommandSecret {
			t.Fatalf("partial update changed identity/key/counter: %+v, %v", row, err)
		}
	}
	rec = requestJSON(t, mux, http.MethodPost, "/api/remote-actions/commands/BURNTX", map[string]any{"command": "TX=OFF"})
	if rec.Code != http.StatusAccepted || captured.To != "BURNTX" || captured.Text != "!RC1:A:1Z:TX=OFF:OLkaJxyKIXBM9SrF" {
		t.Fatalf("signature changed after rename: %d %+v", rec.Code, captured)
	}
}
