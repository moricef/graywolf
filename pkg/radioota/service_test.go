package radioota

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const secret = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func settings() Settings {
	p := Profile{Frequency: 433775000, Bandwidth: 125000, SF: 12, CR: 5, Power: 2}
	return Settings{TargetBoard: "ttgo-lora32-v21", Bridge: "SIMULATED-NO-SERIAL", Normal: p, Transfer: Profile{Frequency: 434550000, Bandwidth: 250000, SF: 8, CR: 6, Power: 3}}
}
func image() []byte {
	b := make([]byte, 512)
	b[0] = 0xe9
	copy(b[32:], []byte{0x32, 0x54, 0xcd, 0xab})
	copy(b[48:80], "4.0.2RXT-OTA-B")
	return b
}
func wait(t *testing.T, s *Service, target string) *Job {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		j := s.Status(target)
		if j != nil && j.State != "running" {
			return j
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}
func arg(args []string, name string) string {
	for i, a := range args {
		if a == name {
			return args[i+1]
		}
	}
	return ""
}
func TestConfirmedSuccessAndImmutableImage(t *testing.T) {
	root := t.TempDir()
	block := make(chan struct{})
	started := make(chan []string, 1)
	s := New(root, func(args []string, out io.Writer) error {
		started <- args
		<-block
		writeSuccess(out)
		return nil
	})
	if e := s.Save("GENATX", settings()); e != nil {
		t.Fatal(e)
	}
	b := image()
	j, e := s.Start("GENATX", "F4MLV-2", "F4MLV-15", secret, b, Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
	if e != nil {
		t.Fatal(e)
	}
	b[0] = 0
	args := <-started
	copyImage, e := os.ReadFile(arg(args, "--image"))
	if e != nil || copyImage[0] != 0xe9 {
		t.Fatal("input was not snapshotted")
	}
	if arg(args, "--target") != "F4MLV-15" || arg(args, "--source") != "F4MLV-2" {
		t.Fatal(args)
	}
	if _, e = s.Start("OTHER", "F4MLV-2", "F4MLV-15", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"}); !errors.Is(e, ErrBusy) {
		t.Fatal("parallel transfer was not rejected", e)
	}
	if e = s.Save("GENATX", settings()); !errors.Is(e, ErrBusy) {
		t.Fatal("settings changed during job")
	}
	raw, _ := json.Marshal(j)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), root) {
		t.Fatal("public job leaked private data")
	}
	close(block)
	done := wait(t, s, "GENATX")
	if done.State != "succeeded" || done.Written != 512 {
		t.Fatal(done)
	}
	if _, e = os.Stat(arg(args, "--key-file")); !os.IsNotExist(e) {
		t.Fatal("transient key retained")
	}
	restarted := New(root, nil)
	if restarted.Status("GENATX").State != "succeeded" {
		t.Fatal("job history missing")
	}
}
func TestExitWithoutConfirmationNeverSucceeds(t *testing.T) {
	for _, exitErr := range []error{nil, errors.New("simulated engine failure")} {
		s := New(t.TempDir(), func(_ []string, out io.Writer) error {
			io.WriteString(out, "{\"event\":\"verified\"}\n")
			return exitErr
		})
		s.Save("X", settings())
		s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
		if j := wait(t, s, "X"); j.State != "failed" {
			t.Fatal(j)
		}
	}
}
func TestPauseAndActivationGuard(t *testing.T) {
	s := New(t.TempDir(), func(args []string, out io.Writer) error {
		for i := 0; i < 2000; i++ {
			if _, e := os.Stat(arg(args, "--pause-file")); e == nil {
				io.WriteString(out, "{\"event\":\"operator_paused\"}\n")
				return errors.New("75")
			}
			time.Sleep(time.Millisecond)
		}
		return errors.New("timeout")
	})
	s.Save("X", settings())
	s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
	if e := s.Pause("X"); e != nil {
		t.Fatal(e)
	}
	if j := wait(t, s, "X"); j.State != "paused" {
		t.Fatal(j)
	}
	confirming := make(chan struct{})
	release := make(chan struct{})
	s.runner = func(_ []string, out io.Writer) error {
		io.WriteString(out, "{\"event\":\"activation_starting\"}\n")
		close(confirming)
		<-release
		writeSuccess(out)
		return nil
	}
	s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
	<-confirming
	for i := 0; i < 200 && s.Status("X").CanPause; i++ {
		time.Sleep(time.Millisecond)
	}
	if e := s.Pause("X"); e == nil {
		t.Fatal("pause accepted after activation")
	}
	close(release)
	wait(t, s, "X")
}
func TestPrivateRouteSecretValidationAndStableCounters(t *testing.T) {
	root := t.TempDir()
	s := New(root, func(args []string, out io.Writer) error {
		writeSuccess(out)
		return nil
	})
	cfg := settings()
	cfg.Via = "RELAY"
	cfg.RouteSecret = secret
	if e := s.Save("X", cfg); e != nil {
		t.Fatal(e)
	}
	public, _ := s.Settings("X")
	if public.RouteSecret != "" || !public.HasRouteSecret {
		t.Fatal(public)
	}
	if e := s.Save("X", public); e != nil {
		t.Fatal("blank secret must preserve saved key", e)
	}
	j, e := s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"})
	if e != nil {
		t.Fatal(e)
	}
	wait(t, s, "X")
	b, _ := os.ReadFile(filepath.Join(j.dir, "profiles.json"))
	if !strings.Contains(string(b), "normal") {
		t.Fatal("profiles missing")
	}
	if e = s.Save("X", Settings{TargetBoard: "ttgo-lora32-v21", Bridge: "SIMULATED"}); e == nil {
		t.Fatal("invalid profiles accepted")
	}
	b = image()
	b[12] = 9
	if e = ValidateImage(b, "ttgo-lora32-v21"); e == nil {
		t.Fatal("wrong chip accepted")
	}
	if _, e = s.Start("X", "TOO-LONG-1", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"}); e == nil {
		t.Fatal("truncated identity accepted")
	}
	counter := filepath.Join(root, "counters", key("N7UV-13\x00N7UV")+".counter")
	if e = write(counter, []byte("123")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"}); e != nil {
		t.Fatal(e)
	}
	wait(t, s, "X")
	if b, _ = os.ReadFile(counter); string(b) != "123" {
		t.Fatal("counter reset")
	}
}

func TestFailedFinalChecksDoNotHideConfirmedFirmware(t *testing.T) {
	s := New(t.TempDir(), func(_ []string, out io.Writer) error {
		writeSuccess(out)
		return errors.New("post-check failed")
	})
	if err := s.Save("X", settings()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("X", "N7UV-13", "N7UV", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"}); err != nil {
		t.Fatal(err)
	}
	j := wait(t, s, "X")
	if j.State != "failed" || !strings.Contains(j.Error, "confirmed over radio") {
		t.Fatal(j)
	}
}

func writeSuccess(w io.Writer) {
	b := image()
	sum := sha256.Sum256(b)
	proof := map[string]any{"pending": true, "running": 0x1f0000, "boot": 0x1f0000, "version": "4.0.2RXT-OTA-B", "image_sha": hex.EncodeToString(sum[:])}
	json.NewEncoder(w).Encode(map[string]any{"event": "boot_pending_confirmation", "reply": proof})
	proof["pending"] = false
	json.NewEncoder(w).Encode(map[string]any{"event": "radio_confirmed", "reply": proof})
	json.NewEncoder(w).Encode(map[string]any{"event": "post_install_verified", "reply": proof})
}

func TestFinalProofRequiredAndChecked(t *testing.T) {
	for _, kind := range []string{"missing", "sha", "version", "partition", "pending", "omitted_pending", "before_confirmation"} {
		t.Run(kind, func(t *testing.T) {
			s := New(t.TempDir(), func(_ []string, w io.Writer) error {
				var events strings.Builder
				writeSuccess(&events)
				lines := strings.Split(strings.TrimSpace(events.String()), "\n")
				if kind == "missing" {
					lines = lines[:2]
				} else if kind == "before_confirmation" {
					lines[1], lines[2] = lines[2], lines[1]
				} else {
					var event map[string]any
					json.Unmarshal([]byte(lines[2]), &event)
					p := event["reply"].(map[string]any)
					switch kind {
					case "sha":
						p["image_sha"] = strings.Repeat("0", 64)
					case "version":
						p["version"] = "wrong"
					case "partition":
						p["boot"] = 0x10000
					case "pending":
						p["pending"] = true
					case "omitted_pending":
						delete(p, "pending")
					}
					b, _ := json.Marshal(event)
					lines[2] = string(b)
				}
				fmt.Fprintln(w, strings.Join(lines, "\n"))
				return nil
			})
			if err := s.Save("X", settings()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Start("X", "F4MLV-2", "F4MLV-15", secret, image(), Options{ExpectedVersion: "4.0.2RXT-OTA-B"}); err != nil {
				t.Fatal(err)
			}
			if j := wait(t, s, "X"); j.State != "failed" {
				t.Fatal(j)
			}
		})
	}
}

func TestOptionsAreExplicitAndRetainedInAudit(t *testing.T) {
	for _, opts := range []Options{{ExpectedVersion: "4.0.2RXT-OTA-B"}, {ExpectedVersion: "4.0.2RXT-OTA-B", ReplaceExisting: true, RequireOffline: true, ExerciseConfirmTimeout: true}} {
		var received []string
		s := New(t.TempDir(), func(args []string, w io.Writer) error {
			received = args
			if opts.RequireOffline {
				fmt.Fprintln(w, `{"event":"offline_before_update"}`)
				fmt.Fprintln(w, `{"event":"offline_after_update"}`)
			}
			if opts.ExerciseConfirmTimeout {
				fmt.Fprintln(w, `{"event":"confirmation_timeout_rollback"}`)
			}
			writeSuccess(w)
			return nil
		})
		if err := s.Save("X", settings()); err != nil {
			t.Fatal(err)
		}
		j, err := s.Start("X", "F4MLV-2", "F4MLV-15", secret, image(), opts)
		if err != nil {
			t.Fatal(err)
		}
		done := wait(t, s, "X")
		if done.State != "succeeded" || !done.PostInstallVerified || done.Final == nil {
			t.Fatal(done)
		}
		for flag, expected := range map[string]bool{"--replace-existing": opts.ReplaceExisting, "--require-offline": opts.RequireOffline, "--exercise-confirm-timeout": opts.ExerciseConfirmTimeout} {
			present := false
			for _, a := range received {
				if a == flag {
					present = true
				}
			}
			if present != expected {
				t.Fatal(flag, received)
			}
		}
		if arg(received, "--block-size") != "128" || strings.Contains(strings.Join(received, " "), "--compress") {
			t.Fatal(received)
		}
		b, err := os.ReadFile(filepath.Join(j.dir, "job.json"))
		if err != nil {
			t.Fatal(err)
		}
		var saved Job
		json.Unmarshal(b, &saved)
		if saved.Options != opts || saved.Source != "F4MLV-2" || saved.SignedTarget != "F4MLV-15" || saved.BlockSize != 128 {
			t.Fatal(saved)
		}
	}
}

func TestRequestedScenarioEvidenceCannotBeMissing(t *testing.T) {
	for _, opts := range []Options{{ExpectedVersion: "4.0.2RXT-OTA-B", RequireOffline: true}, {ExpectedVersion: "4.0.2RXT-OTA-B", ExerciseConfirmTimeout: true}} {
		s := New(t.TempDir(), func(_ []string, w io.Writer) error { writeSuccess(w); return nil })
		s.Save("X", settings())
		s.Start("X", "F4MLV-2", "F4MLV-15", secret, image(), opts)
		if j := wait(t, s, "X"); j.State != "failed" {
			t.Fatal(j)
		}
	}
}

func TestExpectedVersionIsRequiredAndPrintable(t *testing.T) {
	for _, version := range []string{"", strings.Repeat("v", 41), "line\nbreak", "non-ascii-é"} {
		if err := (Options{ExpectedVersion: version}).Validate(); err == nil {
			t.Fatal("accepted", version)
		}
	}
	if err := (Options{ExpectedVersion: "4.0.2RXT-OTA-B"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetBoardRequiredAndImageFamily(t *testing.T) {
	for _, model := range []string{"", "unknown"} {
		cfg := settings()
		cfg.TargetBoard = model
		if cfg.Validate() == nil {
			t.Fatalf("accepted board %q", model)
		}
	}
	for _, tc := range []struct {
		model string
		chip  byte
		id    string
	}{
		{"ttgo-lora32-v21", 0, "0x54474732"},
		{"heltec-v3", 9, "0x484C5633"},
		{"qrp-labs-lightgateway-plus", 9, "0x51524C50"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			b := image()
			b[12] = tc.chip
			if err := ValidateImage(b, tc.model); err != nil {
				t.Fatal(err)
			}
			wrong := append([]byte{}, b...)
			wrong[12] = tc.chip ^ 9
			if ValidateImage(wrong, tc.model) == nil {
				t.Fatal("accepted wrong chip")
			}
			called := make(chan []string, 1)
			root := t.TempDir()
			s := New(root, func(args []string, out io.Writer) error { called <- args; return errors.New("mock stop") })
			cfg := settings()
			cfg.TargetBoard = tc.model
			if err := s.Save("X", cfg); err != nil {
				t.Fatal(err)
			}
			saved, err := New(root, nil).Settings("X")
			if err != nil || saved.TargetBoard != tc.model {
				t.Fatal(saved, err)
			}
			job, err := s.Start("X", "F4MLV-2", "F4MLV-15", secret, b, Options{ExpectedVersion: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if args := <-called; arg(args, "--board-id") != tc.id {
				t.Fatal(args)
			}
			wait(t, s, "X")
			persisted := New(root, nil).Status("X")
			if persisted.TargetBoard != tc.model || persisted.BoardID != job.BoardID || job.BoardID == 0 {
				t.Fatal(persisted)
			}
		})
	}
}

func TestFormatFailureExplainedByOperation(t *testing.T) {
	for _, tc := range []struct {
		op   int
		want string
	}{{15, "manifest"}, {16, "offset"}, {8, "operation"}} {
		s := New(t.TempDir(), func(_ []string, out io.Writer) error {
			fmt.Fprintf(out, "{\"event\":\"protocol_rejected\",\"op\":%d,\"status\":1,\"offset\":128,\"payload_bytes\":40}\n", tc.op)
			return errors.New("mock rejection")
		})
		s.Save("X", settings())
		if _, err := s.Start("X", "F4MLV-2", "F4MLV-15", secret, image(), Options{ExpectedVersion: "test"}); err != nil {
			t.Fatal(err)
		}
		j := wait(t, s, "X")
		if j.State != "failed" || !strings.Contains(j.Error, tc.want) {
			t.Fatal(j)
		}
	}
}

func TestNativeServiceAuditWithoutScriptExtraction(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := newFakeOTA(0x54474732)
	p.image = image()
	root := t.TempDir()
	s := New(root, func(args []string, out io.Writer) error {
		return runNative(args, out, func(string) (otaPort, error) { return p, nil }, p.clock)
	})
	if err := s.Save("X", settings()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("X", p.source, p.target, secret, p.image, Options{ExpectedVersion: "new"}); err != nil {
		t.Fatal(err)
	}
	j := wait(t, s, "X")
	if j.State != "succeeded" || j.Engine != "native-go" || !j.PostInstallVerified {
		t.Fatal(j)
	}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".py") {
			t.Error("extracted Python", path)
		}
		if d.Name() == "target.key" || d.Name() == "route.key" {
			t.Error("retained temporary key", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
