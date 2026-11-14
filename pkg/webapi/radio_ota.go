package webapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/radioota"
)

func (s *Server) ota() *radioota.Service {
	s.otaInit.Do(func() {
		path := s.configDBPath
		if path == "" {
			path = "graywolf.db"
		}
		s.radioOTA = radioota.New(filepath.Join(filepath.Dir(path), "radio-ota"), nil)
		s.radioOTA.SetControlKeyResolver(func(rf, signed string) (string, error) {
			if s.remoteActions == nil {
				return "", errors.New("authenticated control is unavailable")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			credential, err := s.remoteActions.Commands().GetByTarget(ctx, rf)
			if err != nil {
				return "", err
			}
			if credential.SignedTarget != signed {
				return "", errors.New("relay signed identity mismatch")
			}
			return credential.SecretBase64URL, nil
		})
		s.radioOTA.SetCommandSigner(func(rf, signed, controller, command string) (string, uint64, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if s.remoteActions == nil {
				return "", 0, errors.New("authenticated control is unavailable")
			}
			credential, err := s.remoteActions.Commands().GetByTarget(ctx, rf)
			if err != nil {
				return "", 0, fmt.Errorf("install a control key for RF recipient %s before preparing this route", rf)
			}
			if credential.SignedTarget != signed {
				return "", 0, fmt.Errorf("signed identity for %s must be %s", rf, signed)
			}
			return s.remoteActions.Commands().ReserveEnvelope(ctx, rf, controller, command)
		})
	})
	return s.radioOTA
}

// GuardRadioConfig serializes changes that could compete for an OTA serial bridge.
func (s *Server) GuardRadioConfig(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.otaPorts.Lock()
		defer s.otaPorts.Unlock()
		if s.ota().Active() {
			conflict(w, "Radio interface settings are locked during the OTA transfer")
			return
		}
		next(w, r)
	}
}

// SetRadioOTATNC2Device supplies the effective TNC2 port, including legacy flags.
func (s *Server) SetRadioOTATNC2Device(f func() string) { s.otaTNC2Device = f }
func samePort(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	clean := func(p string) string {
		if v, e := filepath.EvalSymlinks(p); e == nil {
			p = v
		}
		if v, e := filepath.Abs(p); e == nil {
			p = v
		}
		return strings.ToLower(filepath.Clean(p))
	}
	return clean(a) == clean(b)
}
func (s *Server) otaBridgeConflict(r *http.Request, port string) error {
	if s.otaTNC2Device != nil && samePort(port, s.otaTNC2Device()) {
		return errors.New("this port is already assigned to the TNC2 transceiver; select a separate USB port for OTA")
	}
	cfg, _, err := s.store.GetTNC2Config(r.Context())
	if err != nil {
		return err
	}
	if samePort(port, cfg.SerialDevice) {
		return errors.New("this port is assigned to TNC2")
	}
	rows, err := s.store.ListKissInterfaces(r.Context())
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Enabled && row.InterfaceType == configstore.KissTypeSerial && samePort(port, row.Device) {
			return errors.New("this port is assigned to an enabled KISS interface")
		}
	}
	gps, err := s.store.GetGPSConfig(r.Context())
	if err != nil {
		return err
	}
	if gps != nil && gps.Enabled && gps.SourceType == "serial" && samePort(port, gps.Device) {
		return errors.New("this port is assigned to GPS")
	}
	beacons, err := s.store.ListBeacons(r.Context())
	if err != nil {
		return err
	}
	for _, beacon := range beacons {
		if samePort(port, beacon.WeatherDevice) {
			return errors.New("this port is assigned to a weather receiver")
		}
	}
	return nil
}
func (s *Server) registerRadioOTA(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/radio-ota/{target}", s.getRadioOTA)
	mux.HandleFunc("PUT /api/radio-ota/{target}/settings", s.saveRadioOTA)
	mux.HandleFunc("POST /api/radio-ota/{target}/firmware", s.startRadioOTA)
	mux.HandleFunc("POST /api/radio-ota/{target}/preflight", s.preflightRadioOTA)
	mux.HandleFunc("POST /api/radio-ota/{target}/pause", s.pauseRadioOTA)
}
func (s *Server) otaTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.requireRemoteActions(w) {
		return "", false
	}
	target := strings.ToUpper(strings.TrimSpace(r.PathValue("target")))
	if _, err := s.remoteActions.Commands().GetByTarget(r.Context(), target); err != nil {
		notFound(w)
		return "", false
	}
	return target, true
}

// @Summary Get radio OTA station settings and progress
// @Tags radio-ota
// @ID getRadioOTA
// @Produce json
// @Param target path string true "Station RF identity"
// @Success 200 {object} map[string]interface{}
// @Security CookieAuth
// @Router /radio-ota/{target} [get]
func (s *Server) getRadioOTA(w http.ResponseWriter, r *http.Request) {
	target, ok := s.otaTarget(w, r)
	if !ok {
		return
	}
	cfg, err := s.ota().Settings(target)
	if err != nil {
		s.internalError(w, r, "read OTA settings", err)
		return
	}
	credential, err := s.remoteActions.Commands().GetByTarget(r.Context(), target)
	if err != nil {
		notFound(w)
		return
	}
	source, err := s.resolveOurCall(r.Context())
	if err != nil {
		s.internalError(w, r, "read controller identity", err)
		return
	}
	stablePorts := []map[string]string{}
	links, _ := filepath.Glob("/dev/serial/by-id/*")
	for _, link := range links {
		device, e := filepath.EvalSymlinks(link)
		if e == nil {
			stablePorts = append(stablePorts, map[string]string{"path": link, "description": "Stable USB serial link → " + filepath.Base(device)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stable_ports": stablePorts, "controller": source, "signed_target": credential.SignedTarget, "settings": cfg, "job": s.ota().Status(target), "engine_ready": true, "engine_error": "", "engine": "native-go"})
}

// @Summary Save radio OTA station settings
// @Tags radio-ota
// @ID saveRadioOTA
// @Accept json
// @Produce json
// @Param target path string true "Station RF identity"
// @Param body body radioota.Settings true "Radio settings; blank route secret preserves existing secret"
// @Success 200 {object} radioota.Settings
// @Security CookieAuth
// @Router /radio-ota/{target}/settings [put]
func (s *Server) saveRadioOTA(w http.ResponseWriter, r *http.Request) {
	target, ok := s.otaTarget(w, r)
	if !ok {
		return
	}
	cfg, err := decodeJSON[radioota.Settings](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.otaPorts.Lock()
	defer s.otaPorts.Unlock()
	if err = s.otaBridgeConflict(r, cfg.Bridge); err != nil {
		conflict(w, err.Error())
		return
	}
	if err = s.ota().Save(target, cfg); err != nil {
		if errors.Is(err, radioota.ErrBusy) {
			conflict(w, err.Error())
		} else {
			badRequest(w, err.Error())
		}
		return
	}
	cfg, _ = s.ota().Settings(target)
	writeJSON(w, http.StatusOK, cfg)
}

// @Summary Upload firmware and start an OTA job on the Graywolf host
// @Tags radio-ota
// @ID startRadioOTA
// @Accept application/octet-stream
// @Produce json
// @Param target path string true "Station RF identity"
// @Param expected_version query string true "Expected firmware version reported by the target after reboot"
// @Param replace_existing query boolean false "Explicitly discard the old inactive candidate and its resume point"
// @Param require_offline query boolean false "Require target Wi-Fi off and no active IP interface"
// @Param exercise_confirm_timeout query boolean false "Validation only: withhold confirmation, observe rollback, reactivate and confirm"
// @Param firmware body string true "ESP32 application firmware binary"
// @Success 202 {object} radioota.Job
// @Security CookieAuth
// @Router /radio-ota/{target}/firmware [post]
func (s *Server) startRadioOTA(w http.ResponseWriter, r *http.Request) {
	s.startRadioOTAJob(w, r, false)
}

// @Summary Prepare and verify a LoRa OTA route without transferring firmware
// @Tags radio-ota
// @ID preflightRadioOTA
// @Accept application/octet-stream
// @Produce json
// @Param target path string true "Station RF identity"
// @Param expected_version query string true "Expected candidate firmware version"
// @Param firmware body string true "Image to validate locally; no firmware blocks transmitted"
// @Success 202 {object} radioota.Job
// @Security CookieAuth
// @Router /radio-ota/{target}/preflight [post]
func (s *Server) preflightRadioOTA(w http.ResponseWriter, r *http.Request) {
	s.startRadioOTAJob(w, r, true)
}

func (s *Server) startRadioOTAJob(w http.ResponseWriter, r *http.Request, preflight bool) {
	target, ok := s.otaTarget(w, r)
	if !ok {
		return
	}
	opts := radioota.Options{PrepareRoute: true, PreflightOnly: preflight, ExpectedVersion: strings.TrimSpace(r.URL.Query().Get("expected_version"))}
	for name, dst := range map[string]*bool{"replace_existing": &opts.ReplaceExisting, "require_offline": &opts.RequireOffline, "exercise_confirm_timeout": &opts.ExerciseConfirmTimeout} {
		if values, present := r.URL.Query()[name]; present {
			if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
				badRequest(w, "invalid boolean option: "+name)
				return
			}
			*dst, _ = strconv.ParseBool(values[0])
		}
	}
	if err := opts.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, radioota.MaxImage)
	image, err := io.ReadAll(r.Body)
	if err != nil {
		badRequest(w, "firmware exceeds the application slot size")
		return
	}
	credential, err := s.remoteActions.Commands().GetByTarget(r.Context(), target)
	if err != nil {
		notFound(w)
		return
	}
	source, err := s.resolveOurCall(r.Context())
	if err != nil {
		s.internalError(w, r, "read controller identity", err)
		return
	}
	s.otaPorts.Lock()
	defer s.otaPorts.Unlock()
	cfg, err := s.ota().Settings(target)
	if err != nil {
		s.internalError(w, r, "read OTA settings", err)
		return
	}
	if err = s.otaBridgeConflict(r, cfg.Bridge); err != nil {
		conflict(w, err.Error())
		return
	}
	if cfg.Via != "" {
		rf := cfg.RelayRecipient
		if rf == "" {
			rf = cfg.Via
		}
		relayCredential, e := s.remoteActions.Commands().GetByTarget(r.Context(), rf)
		if e != nil || relayCredential.SignedTarget != cfg.Via {
			badRequest(w, "install the relay control key with its RF recipient and signed Callsign-SSID before preparing this route")
			return
		}
		if rf == target || cfg.Via == credential.SignedTarget || cfg.Via == source {
			badRequest(w, "controller, relay and target must be distinct stations")
			return
		}
	}
	if preflight {
		opts.ReplaceExisting = false
		opts.RequireOffline = false
		opts.ExerciseConfirmTimeout = false
	}
	job, err := s.ota().Start(target, source, credential.SignedTarget, credential.SecretBase64URL, image, opts)
	if err != nil {
		if errors.Is(err, radioota.ErrBusy) {
			conflict(w, err.Error())
		} else {
			badRequest(w, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// @Summary Pause a radio OTA job before activation
// @Tags radio-ota
// @ID pauseRadioOTA
// @Produce json
// @Param target path string true "Station RF identity"
// @Success 200 {object} radioota.Job
// @Security CookieAuth
// @Router /radio-ota/{target}/pause [post]
func (s *Server) pauseRadioOTA(w http.ResponseWriter, r *http.Request) {
	target, ok := s.otaTarget(w, r)
	if !ok {
		return
	}
	if err := s.ota().Pause(target); err != nil {
		conflict(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.ota().Status(target))
}
