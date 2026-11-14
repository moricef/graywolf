package webapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/radioota"
)

func otaTestConfig() radioota.Settings {
	p := radioota.Profile{Frequency: 433775000, Bandwidth: 125000, SF: 12, CR: 5, Power: 2}
	return radioota.Settings{TargetBoard: "ttgo-lora32-v21", Bridge: "SIMULATED-NO-RADIO", Normal: p, Transfer: p}
}
func otaTestImage() []byte {
	b := make([]byte, 512)
	b[0] = 0xe9
	copy(b[32:], []byte{0x32, 0x54, 0xcd, 0xab})
	copy(b[48:80], "4.0.2RXT-OTA-B")
	return b
}
func TestRadioOTAUploadSurvivesRequestAndUsesStoredIdentity(t *testing.T) {
	srv, mux := newRemoteCommandTestServer(t, nil)
	created := createTestCommandCredential(t, mux)
	root := t.TempDir()
	release := make(chan struct{})
	defer func() {
		close(release)
		until := time.Now().Add(2 * time.Second)
		for srv.ota().Active() && time.Now().Before(until) {
			time.Sleep(time.Millisecond)
		}
	}()
	started := make(chan []string, 1)
	srv.otaInit.Do(func() {
		srv.radioOTA = radioota.New(root, func(args []string, out io.Writer) error {
			started <- args
			<-release
			io.WriteString(out, "{\"event\":\"radio_confirmed\"}\n")
			return nil
		})
	})
	cfg := otaTestConfig()
	cfg.Via = "F4MLV-10"
	relay := requestJSON(t, mux, "POST", "/api/remote-actions/command-credentials", map[string]any{"name": "relay", "target_call": "F4MLV-10", "signed_target": "F4MLV-10", "secret_b64url": testRemoteCommandSecret})
	if relay.Code != 201 {
		t.Fatal(relay.Body.String())
	}
	cfg.RouteSecret = testRemoteCommandSecret
	path := "/api/radio-ota/" + created.TargetCall
	saved := requestJSON(t, mux, http.MethodPut, path+"/settings", cfg)
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	if strings.Contains(saved.Body.String(), testRemoteCommandSecret) {
		t.Fatal("route key exposed")
	}
	req := httptest.NewRequest("POST", path+"/firmware?expected_version=4.0.2RXT-OTA-B&replace_existing=true&require_offline=true&exercise_confirm_timeout=true", bytes.NewReader(otaTestImage()))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	// The handler has returned; the worker remains alive and owns the bridge.
	args := <-started
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--target F4MLV-15") || !strings.Contains(joined, "--source F4MLV-2") {
		t.Fatal(joined)
	}
	for _, flag := range []string{"--replace-existing", "--require-offline", "--exercise-confirm-timeout"} {
		if !strings.Contains(joined, flag) {
			t.Fatal("missing option", joined)
		}
	}
	if !strings.Contains(joined, "--block-size 128") {
		t.Fatal(joined)
	}
	for i, a := range args {
		if a == "--key-file" {
			b, e := os.ReadFile(args[i+1])
			if e != nil || string(b) != testRemoteCommandSecret {
				t.Fatal("credential not passed privately")
			}
		}
	}
	blocked := requestJSON(t, mux, "PUT", "/api/gps", map[string]any{"enabled": false})
	if blocked.Code != 409 {
		t.Fatal("GPS change was not locked", blocked.Code)
	}
	blocked = requestJSON(t, mux, "POST", "/api/remote-actions/commands/"+created.TargetCall, map[string]any{"command": "TX=OFF"})
	if blocked.Code != 409 {
		t.Fatal("control command was not locked", blocked.Code)
	}
	paused := requestJSON(t, mux, "POST", path+"/pause", nil)
	if paused.Code != 200 {
		t.Fatal(paused.Code, paused.Body.String())
	}
	var job radioota.Job
	json.Unmarshal(rec.Body.Bytes(), &job)
	if _, err := os.Stat(filepath.Join(root, "transfers", job.ID, "pause.request")); err != nil {
		t.Fatal("pause request missing", err)
	}
}
func TestRadioOTARejectsSharedPortInvalidImageAndMissingKey(t *testing.T) {
	srv, mux := newRemoteCommandTestServer(t, nil)
	createTestCommandCredential(t, mux)
	srv.otaInit.Do(func() {
		srv.radioOTA = radioota.New(t.TempDir(), func([]string, io.Writer) error { t.Error("worker must not start"); return nil })
	})
	srv.SetRadioOTATNC2Device(func() string { return "COM4" })
	cfg := otaTestConfig()
	cfg.Bridge = "com4"
	rec := requestJSON(t, mux, "PUT", "/api/radio-ota/F4MLV-15/settings", cfg)
	if rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/radio-ota/F4MLV-15/firmware", strings.NewReader("not an ESP32 image"))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	rec = requestJSON(t, mux, "GET", "/api/radio-ota/UNKNOWN", nil)
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
}
func TestRadioOTASettingsSurviveRestartWithoutSecretExposure(t *testing.T) {
	root := t.TempDir()
	s := radioota.New(root, nil)
	cfg := otaTestConfig()
	cfg.Via = "RELAY"
	cfg.RouteSecret = testRemoteCommandSecret
	if err := s.Save("GENATX", cfg); err != nil {
		t.Fatal(err)
	}
	reloaded := radioota.New(root, nil)
	got, err := reloaded.Settings("GENATX")
	if err != nil || !got.HasRouteSecret || got.RouteSecret != "" {
		t.Fatal(got, err)
	}
}
func TestSameOTAPortResolvesSymlink(t *testing.T) {
	if !samePort("COM4", "com4") {
		t.Fatal("Windows port aliases")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	os.WriteFile(real, nil, 0600)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip(err)
	}
	if !samePort(real, alias) {
		t.Fatal("symlink bypassed port exclusion")
	}
}

// A fast worker completing before the first status poll must still be visible.
func TestRadioOTAFastCompletion(t *testing.T) {
	s := radioota.New(t.TempDir(), func(_ []string, w io.Writer) error {
		sum := sha256.Sum256(otaTestImage())
		proof := map[string]any{"pending": true, "running": 0x1f0000, "boot": 0x1f0000, "version": "4.0.2RXT-OTA-B", "image_sha": hex.EncodeToString(sum[:])}
		json.NewEncoder(w).Encode(map[string]any{"event": "boot_pending_confirmation", "reply": proof})
		proof["pending"] = false
		for _, event := range []string{"radio_confirmed", "post_install_verified"} {
			json.NewEncoder(w).Encode(map[string]any{"event": event, "reply": proof})
		}
		return nil
	})
	s.Save("X", otaTestConfig())
	s.Start("X", "N7UV-13", "N7UV", testRemoteCommandSecret, otaTestImage(), radioota.Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		j := s.Status("X")
		if j.State == "succeeded" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("success lost")
}

func TestRadioOTANormalOptionsAndExplicitVersion(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	srv, mux := newRemoteCommandTestServer(t, nil)
	rec := requestJSON(t, mux, "POST", "/api/remote-actions/command-credentials", map[string]any{"name": "tactical", "target_call": "GENATX", "signed_target": "F4MLV-15", "secret_b64url": testRemoteCommandSecret})
	if rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	started := make(chan []string, 1)
	srv.otaInit.Do(func() {
		srv.radioOTA = radioota.New(t.TempDir(), func(args []string, w io.Writer) error { started <- args; return nil })
	})
	if err := srv.ota().Save("GENATX", otaTestConfig()); err != nil {
		t.Fatal(err)
	}
	rec = requestJSON(t, mux, "GET", "/api/radio-ota/GENATX", nil)
	var status struct {
		Engine       string `json:"engine"`
		EngineReady  bool   `json:"engine_ready"`
		Controller   string `json:"controller"`
		SignedTarget string `json:"signed_target"`
	}
	json.Unmarshal(rec.Body.Bytes(), &status)
	if rec.Code != 200 || status.Engine != "native-go" || !status.EngineReady || status.Controller != "F4MLV-2" || status.SignedTarget != "F4MLV-15" || strings.Contains(rec.Body.String(), testRemoteCommandSecret) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	for _, query := range []string{"", "?expected_version=new&replace_existing=maybe", "?expected_version=new&require_offline=true&require_offline=false"} {
		req := httptest.NewRequest("POST", "/api/radio-ota/GENATX/firmware"+query, bytes.NewReader(otaTestImage()))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatal(query, rec.Code)
		}
	}
	select {
	case <-started:
		t.Fatal("invalid request started worker")
	default:
	}
	req := httptest.NewRequest("POST", "/api/radio-ota/GENATX/firmware?expected_version=4.0.2RXT-OTA-B", bytes.NewReader(otaTestImage()))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	args := <-started
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--replace-existing", "--require-offline", "--exercise-confirm-timeout", "--compress"} {
		if strings.Contains(joined, flag) {
			t.Fatal(joined)
		}
	}
	if !strings.Contains(joined, "--target F4MLV-15") || !strings.Contains(joined, "--source F4MLV-2") {
		t.Fatal(joined)
	}
	until := time.Now().Add(time.Second)
	for srv.ota().Active() && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
}

func TestRadioOTABoardSelectionAndFamily(t *testing.T) {
	for _, model := range []string{"ttgo-lora32-v21", "heltec-v3", "qrp-labs-lightgateway-plus"} {
		t.Run(model, func(t *testing.T) {
			srv, mux := newRemoteCommandTestServer(t, nil)
			created := createTestCommandCredential(t, mux)
			started := make(chan []string, 1)
			srv.otaInit.Do(func() {
				srv.radioOTA = radioota.New(t.TempDir(), func(args []string, _ io.Writer) error { started <- args; return nil })
			})
			path := "/api/radio-ota/" + created.TargetCall
			cfg := otaTestConfig()
			cfg.TargetBoard = ""
			if rec := requestJSON(t, mux, "PUT", path+"/settings", cfg); rec.Code != 400 {
				t.Fatal("missing board accepted", rec.Code)
			}
			cfg.TargetBoard = model
			if rec := requestJSON(t, mux, "PUT", path+"/settings", cfg); rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			board, _ := radioota.TargetBoard(model)
			for _, valid := range []bool{false, true} {
				image := otaTestImage()
				image[12] = board.Chip
				if !valid {
					image[12] ^= 9
				}
				req := httptest.NewRequest("POST", path+"/firmware?expected_version=test", bytes.NewReader(image))
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if !valid {
					if rec.Code != 400 {
						t.Fatal("wrong family accepted", rec.Code)
					}
					continue
				}
				if rec.Code != 202 {
					t.Fatal(rec.Code, rec.Body.String())
				}
				select {
				case args := <-started:
					if !strings.Contains(strings.Join(args, " "), "--board-id 0x") {
						t.Fatal(args)
					}
				case <-time.After(time.Second):
					t.Fatal("worker missing")
				}
				until := time.Now().Add(time.Second)
				for srv.ota().Active() && time.Now().Before(until) {
					time.Sleep(time.Millisecond)
				}
			}
		})
	}
}

func TestRadioOTAPreflightAndRouteReservations(t *testing.T) {
	srv, mux := newRemoteCommandTestServer(t, nil)
	created := createTestCommandCredential(t, mux)
	relay := requestJSON(t, mux, "POST", "/api/remote-actions/command-credentials", map[string]any{"name": "relay tactical", "target_call": "RELAYX", "signed_target": "F4MLV-10", "secret_b64url": testRemoteCommandSecret})
	if relay.Code != 201 {
		t.Fatal(relay.Body.String())
	}
	var relayMeta struct {
		ID uint `json:"id"`
	}
	json.Unmarshal(relay.Body.Bytes(), &relayMeta)
	started := make(chan []string, 1)
	release := make(chan struct{})
	defer func() {
		close(release)
		end := time.Now().Add(time.Second)
		for srv.ota().Active() && time.Now().Before(end) {
			time.Sleep(time.Millisecond)
		}
	}()
	srv.otaInit.Do(func() {
		srv.radioOTA = radioota.New(t.TempDir(), func(args []string, out io.Writer) error {
			started <- args
			<-release
			io.WriteString(out, "{\"event\":\"route_authenticated\"}\n{\"event\":\"preflight_session_released\"}\n{\"event\":\"preflight_verified\"}\n")
			return nil
		})
	})
	cfg := otaTestConfig()
	cfg.Via = "F4MLV-10"
	cfg.RelayRecipient = "RELAYX"
	cfg.RouteSecret = testRemoteCommandSecret
	path := "/api/radio-ota/" + created.TargetCall
	if rec := requestJSON(t, mux, "PUT", path+"/settings", cfg); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("POST", path+"/preflight?expected_version=new&replace_existing=true", bytes.NewReader(otaTestImage()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	args := <-started
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--preflight-only") || !strings.Contains(joined, "--prepare-route") || !strings.Contains(joined, "--relay-recipient RELAYX") || strings.Contains(joined, "--replace-existing") {
		t.Fatal(joined)
	}
	for _, target := range []string{"RELAYX", created.TargetCall} {
		blocked := requestJSON(t, mux, "POST", "/api/remote-actions/commands/"+target, map[string]any{"command": "TX=OFF"})
		if blocked.Code != 409 {
			t.Fatal("route member command not locked", target, blocked.Code)
		}
	}
	blocked := requestJSON(t, mux, "PUT", fmt.Sprintf("/api/remote-actions/command-credentials/%d", relayMeta.ID), map[string]any{"name": "changed"})
	if blocked.Code != 409 {
		t.Fatal("relay credential changed during check", blocked.Code)
	}
	blocked = requestJSON(t, mux, "DELETE", fmt.Sprintf("/api/remote-actions/command-credentials/%d", relayMeta.ID), nil)
	if blocked.Code != 409 {
		t.Fatal("relay credential removed during check", blocked.Code)
	}
}
