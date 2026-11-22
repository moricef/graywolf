// Package radioota implements native Go LoRa OTA behind Graywolf's WebUI.
package radioota

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var rfRecipient = regexp.MustCompile(`^[A-Z0-9]{1,6}(-[A-Z0-9]{1,2})?$`)
var address = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,7}$`)
var ErrBusy = errors.New("an OTA transfer is already running")

const MaxImage = 1966080

type Profile struct {
	Frequency uint32 `json:"frequency_hz"`
	Bandwidth uint32 `json:"bandwidth_hz"`
	SF        int    `json:"spreading_factor"`
	CR        int    `json:"coding_rate"`
	Power     int    `json:"power_dbm"`
}

func (p Profile) Valid() bool {
	return p.Frequency >= 420000000 && p.Frequency <= 450000000 && (p.Bandwidth == 7800 || p.Bandwidth == 10400 || p.Bandwidth == 15600 || p.Bandwidth == 20800 || p.Bandwidth == 31250 || p.Bandwidth == 41700 || p.Bandwidth == 62500 || p.Bandwidth == 125000 || p.Bandwidth == 250000 || p.Bandwidth == 500000) && p.SF >= 5 && p.SF <= 12 && p.CR >= 5 && p.CR <= 8 && p.Power >= 2 && p.Power <= 20
}

func validateTargetProfiles(board uint32, service, transfer Profile) error {
	if board == 0x54474732 && (service.SF < 7 || transfer.SF < 7) {
		return errors.New("TTGO LoRa32 V2.1 / SX1278 requires SF7-SF12 for OTA; SF5/SF6 require SX126x radios on the entire route")
	}
	return nil
}

// Board identifies the target firmware variant, independently of the USB controller.
type Board struct {
	ID   uint32
	Chip byte
}

func TargetBoard(model string) (Board, error) {
	switch model {
	case "ttgo-lora32-v21":
		return Board{0x54474732, 0}, nil
	case "heltec-v3":
		return Board{0x484C5633, 9}, nil
	case "qrp-labs-lightgateway-plus":
		return Board{0x51524C50, 9}, nil
	default:
		return Board{}, errors.New("select the target board: TTGO LoRa32 V2.1, Heltec V3 or QRP Labs LightGateway Plus")
	}
}

// Hop is a relay after the first one: its Callsign-SSID (route identity and
// signed control identity) and, when different, its on-air tactical name.
type Hop struct {
	Callsign  string `json:"callsign"`
	Recipient string `json:"recipient,omitempty"`
}

type Settings struct {
	RelayRecipient string `json:"relay_recipient,omitempty"`
	// Chain lists further relays after Via, nearest first, towards the target.
	Chain          []Hop  `json:"chain,omitempty"`
	TargetBoard    string `json:"target_board"`
	Bridge         string `json:"bridge"`
	Via            string `json:"via"`
	RouteSecret    string `json:"route_secret,omitempty"`
	HasRouteSecret bool   `json:"has_route_secret"`
	// RouteKeyFingerprint matches the fingerprint the iGate WebUI shows for its route key.
	RouteKeyFingerprint string   `json:"route_key_fingerprint,omitempty"`
	Normal              Profile  `json:"normal"`
	TargetNormal        *Profile `json:"target_normal,omitempty"`
	Transfer            Profile  `json:"transfer"`
}

func (s Settings) Validate() error {
	board, err := TargetBoard(s.TargetBoard)
	if err != nil {
		return err
	}
	if strings.HasPrefix(s.Bridge, "/dev/ttyACM") || strings.HasPrefix(s.Bridge, "/dev/ttyUSB") {
		return errors.New("select the controller stable /dev/serial/by-id/ link, not a changing ttyACM/ttyUSB number")
	}
	if strings.TrimSpace(s.Bridge) == "" || strings.ContainsAny(s.Bridge, "\r\n\x00") {
		return errors.New("select the controller USB port for its integrated OTA bridge")
	}
	if s.RelayRecipient != "" && !rfRecipient.MatchString(s.RelayRecipient) {
		return errors.New("invalid relay RF recipient")
	}

	if s.Via != "" && !address.MatchString(s.Via) {
		return errors.New("relay identity must contain 1–8 uppercase letters, digits or hyphens")
	}
	if !s.Normal.Valid() || !s.Transfer.Valid() {
		return errors.New("invalid 70 cm LoRa radio profile (SF5-SF12)")
	}
	if s.TargetNormal != nil && !s.TargetNormal.Valid() {
		return errors.New("invalid target service radio profile")
	}
	service := s.Normal
	if s.TargetNormal != nil {
		service = *s.TargetNormal
	}
	if err := validateTargetProfiles(board.ID, service, s.Transfer); err != nil {
		return err
	}
	if s.TargetNormal != nil && *s.TargetNormal != s.Normal && s.Via == "" {
		return errors.New("different target service profiles currently require a relay")
	}
	if len(s.Chain) > 0 {
		if s.Via == "" {
			return errors.New("enter the first relay before further relays")
		}
		if s.TargetNormal != nil && *s.TargetNormal != s.Normal {
			return errors.New("different service profiles are coordinated through a single relay only")
		}
		if _, err := parseChain(s.chainArg(), "", "", s.Via); err != nil {
			return err
		}
	}
	if s.Via != "" {
		if err := validSecret(s.RouteSecret); err != nil {
			return errors.New("a 43-character route secret is required for relay transfers")
		}
	}
	return nil
}

// chainArg encodes Chain for the engine as CALL[:RFNAME],...
func (s Settings) chainArg() string {
	var items []string
	for _, h := range s.Chain {
		item := h.Callsign
		if h.Recipient != "" && h.Recipient != h.Callsign {
			item += ":" + h.Recipient
		}
		items = append(items, item)
	}
	return strings.Join(items, ",")
}
func validSecret(s string) error {
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) != 32 {
		return errors.New("invalid device secret")
	}
	return nil
}

// ValidateImage checks the chip family; the authenticated receiver additionally
// checks the board ID in the manifest. ESP32-S3 alone cannot distinguish Heltec/QRP.
func ValidateImage(b []byte, model string) error {
	board, err := TargetBoard(model)
	if err != nil {
		return err
	}
	if len(b) < 256 || len(b) > MaxImage || b[0] != 0xe9 || b[12] != board.Chip || b[13] != 0 || !bytes.Equal(b[32:36], []byte{0x32, 0x54, 0xcd, 0xab}) {
		return fmt.Errorf("select an OTA application firmware.bin for %s (wrong image format, size or ESP32 chip family)", model)
	}
	return nil
}

// Options apply only to this attempt. Destructive replacement is never inferred.
type Options struct {
	PrepareRoute           bool   `json:"prepare_route"`
	PreflightOnly          bool   `json:"preflight_only"`
	ExpectedVersion        string `json:"expected_version"`
	ReplaceExisting        bool   `json:"replace_existing"`
	RequireOffline         bool   `json:"require_offline"`
	ExerciseConfirmTimeout bool   `json:"exercise_confirm_timeout"`
}

func (o Options) Validate() error {
	if o.ExpectedVersion == "" || len(o.ExpectedVersion) > 40 {
		return errors.New("enter the expected firmware version (1–40 ASCII characters)")
	}
	for _, c := range []byte(o.ExpectedVersion) {
		if c < 32 || c > 126 {
			return errors.New("expected firmware version must be printable ASCII")
		}
	}
	return nil
}

type BootProof struct {
	Pending  *bool  `json:"pending"`
	Running  uint32 `json:"running"`
	Boot     uint32 `json:"boot"`
	Version  string `json:"version"`
	ImageSHA string `json:"image_sha"`
}

// Runner exists so the complete WebUI lifecycle can be tested without opening hardware.
type Runner func(args []string, output io.Writer) error

type Job struct {
	RelayServiceRestored      bool       `json:"relay_service_restored"`
	TargetServiceVerified     bool       `json:"target_service_verified"`
	ControllerServiceRestored bool       `json:"controller_service_restored"`
	RouteProof                *BootProof `json:"route_proof,omitempty"`
	RolesChanged              []string   `json:"roles_changed,omitempty"`
	Relay                     string     `json:"relay,omitempty"`
	RelayRecipient            string     `json:"relay_recipient,omitempty"`
	Chain                     []Hop      `json:"chain,omitempty"`
	RouteAuthenticated        bool       `json:"route_authenticated"`
	SessionReleased           bool       `json:"session_released"`
	PreflightVerified         bool       `json:"preflight_verified"`
	Engine                    string     `json:"engine"`
	BoardID                   uint32     `json:"board_id"`
	TargetBoard               string     `json:"target_board"`
	Source                    string     `json:"source"`
	SignedTarget              string     `json:"signed_target"`
	ExpectedVersion           string     `json:"expected_version"`
	BlockSize                 int        `json:"block_size"`
	Options                   Options    `json:"options"`
	PostInstallVerified       bool       `json:"post_install_verified"`
	Final                     *BootProof `json:"final,omitempty"`
	ID                        string     `json:"id"`
	Target                    string     `json:"target"`
	State                     string     `json:"state"`
	Phase                     string     `json:"phase"`
	Written                   int        `json:"written"`
	Total                     int        `json:"total"`
	SHA256                    string     `json:"sha256"`
	Error                     string     `json:"error,omitempty"`
	CanPause                  bool       `json:"can_pause"`
	Started                   time.Time  `json:"started"`
	dir                       string
	confirmed                 bool
	bootProof                 *BootProof
	rollbackObserved          bool
	offlineBefore             bool
	offlineAfter              bool
}
type Service struct {
	mu         sync.Mutex
	root       string
	runner     Runner
	signer     CommandSigner
	controlKey func(string, string) (string, error)
	jobs       map[string]*Job
	active     string
}

func (s *Service) SetControlKeyResolver(resolve func(string, string) (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.controlKey = resolve
}

func New(root string, runner Runner) *Service {
	s := &Service{root: root, runner: runner, jobs: make(map[string]*Job)}
	paths, _ := filepath.Glob(filepath.Join(root, "transfers", "*", "job.json"))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var j Job
		if json.Unmarshal(b, &j) != nil {
			continue
		}
		j.dir = filepath.Dir(path)
		if j.State == "running" {
			j.State = "interrupted"
			j.CanPause = false
			j.Error = "Graywolf restarted during this update. Its completion has not been confirmed."
		}
		if old := s.jobs[j.Target]; old == nil || j.Started.After(old.Started) {
			copy := j
			s.jobs[j.Target] = &copy
		}
	}
	return s
}
func key(target string) string { s := sha256.Sum256([]byte(target)); return hex.EncodeToString(s[:]) }
func write(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".ota-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(tmp, path)
}
func (s *Service) load(target string) (Settings, error) {
	var cfg Settings
	b, e := os.ReadFile(filepath.Join(s.root, "stations", key(target)+".json"))
	if os.IsNotExist(e) {
		return cfg, nil
	}
	if e != nil {
		return cfg, e
	}
	e = json.Unmarshal(b, &cfg)
	return cfg, e
}
func (s *Service) Settings(target string) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, e := s.load(target)
	cfg.HasRouteSecret = cfg.RouteSecret != ""
	cfg.RouteKeyFingerprint = keyFingerprint(cfg.RouteSecret)
	cfg.RouteSecret = ""
	return cfg, e
}
func (s *Service) Save(target string, cfg Settings) error {
	cfg.Via = strings.ToUpper(strings.TrimSpace(cfg.Via))
	cfg.RelayRecipient = strings.ToUpper(strings.TrimSpace(cfg.RelayRecipient))
	chain := cfg.Chain[:0:0]
	for _, h := range cfg.Chain {
		h.Callsign = strings.ToUpper(strings.TrimSpace(h.Callsign))
		h.Recipient = strings.ToUpper(strings.TrimSpace(h.Recipient))
		if h.Recipient == h.Callsign {
			h.Recipient = ""
		}
		if h.Callsign != "" || h.Recipient != "" {
			chain = append(chain, h)
		}
	}
	cfg.Chain = chain
	if cfg.Via == "" {
		cfg.RelayRecipient = ""
		cfg.Chain = nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return ErrBusy
	}
	old, e := s.load(target)
	if e != nil {
		return e
	}
	if cfg.RouteSecret == "" && cfg.Via == old.Via {
		cfg.RouteSecret = old.RouteSecret
	}
	if cfg.Via == "" {
		cfg.RouteSecret = ""
	}
	cfg.HasRouteSecret = false
	cfg.RouteKeyFingerprint = ""
	if e = cfg.Validate(); e != nil {
		return e
	}
	b, e := json.Marshal(cfg)
	if e != nil {
		return e
	}
	if err := write(filepath.Join(s.root, "stations", key(target)+".json"), b); err != nil {
		return err
	}
	if j := s.jobs[target]; j != nil && j.State == "ready" {
		j.State = "stale"
		j.Phase = "stale"
		j.Error = "OTA settings changed after the route check. Check preparation again."
		return s.persist(j)
	}
	return nil
}
func (s *Service) Active() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.active != "" }
func (s *Service) Status(target string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := s.jobs[target]; j != nil {
		copy := *j
		copy.RolesChanged = append([]string{}, j.RolesChanged...)
		return &copy
	}
	return nil
}
func (s *Service) Pause(target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[target]
	if j == nil || j.State != "running" || !j.CanPause {
		return errors.New("no pausable transfer for this station")
	}
	if e := write(filepath.Join(j.dir, "pause.request"), []byte("pause\n")); e != nil {
		return e
	}
	j.CanPause = false
	j.Phase = "pausing"
	return nil
}

func (s *Service) Start(target, source, signedTarget, secret string, image []byte, opts Options) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != "" {
		return nil, ErrBusy
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if !address.MatchString(source) || !address.MatchString(signedTarget) {
		return nil, errors.New("binary OTA needs controller and station Callsign-SSID identities of at most 8 characters")
	}
	if e := validSecret(secret); e != nil {
		return nil, e
	}
	cfg, e := s.load(target)
	if e != nil {
		return nil, e
	}
	if e = cfg.Validate(); e != nil {
		return nil, e
	}
	if e = ValidateImage(image, cfg.TargetBoard); e != nil {
		return nil, e
	}
	board, _ := TargetBoard(cfg.TargetBoard)
	runner := s.runner
	if runner == nil {
		signer := s.signer
		runner = func(args []string, out io.Writer) error {
			return runNative(args, out, openOTAPort, wallClock{}, preparationConfig{Signer: signer})
		}
	}
	idBytes := make([]byte, 16)
	if _, e = rand.Read(idBytes); e != nil {
		return nil, e
	}
	id := hex.EncodeToString(idBytes)
	dir := filepath.Join(s.root, "transfers", id)
	files := map[string][]byte{"firmware.bin": image, "target.key": []byte(secret)}
	profiles, _ := json.Marshal(struct {
		Normal       Profile  `json:"normal"`
		Transfer     Profile  `json:"transfer"`
		TargetNormal *Profile `json:"target_normal,omitempty"`
	}{cfg.Normal, cfg.Transfer, cfg.TargetNormal})
	files["profiles.json"] = profiles
	if cfg.Via != "" {
		files["route.key"] = []byte(cfg.RouteSecret)
		if cfg.TargetNormal != nil && *cfg.TargetNormal != cfg.Normal {
			if s.controlKey == nil {
				return nil, errors.New("relay control key resolver is unavailable")
			}
			rf := cfg.RelayRecipient
			if rf == "" {
				rf = cfg.Via
			}
			secret, err := s.controlKey(rf, cfg.Via)
			if err != nil {
				return nil, err
			}
			if err = validSecret(secret); err != nil {
				return nil, err
			}
			files["relay.key"] = []byte(secret)
		}
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	for name, b := range files {
		if e = write(filepath.Join(dir, name), b); e != nil {
			os.RemoveAll(dir)
			return nil, e
		}
	}
	counter := filepath.Join(s.root, "counters", key(source+"\x00"+signedTarget)+".counter")
	if e = os.MkdirAll(filepath.Dir(counter), 0700); e != nil {
		os.RemoveAll(dir)
		return nil, e
	}
	args := []string{"--expected-version", opts.ExpectedVersion, "--bridge", cfg.Bridge, "--source", source, "--target", signedTarget, "--key-file", filepath.Join(dir, "target.key"), "--counter-file", counter, "--image", filepath.Join(dir, "firmware.bin"), "--log", filepath.Join(dir, "transfer.jsonl"), "--pause-file", filepath.Join(dir, "pause.request"), "--windowed", "--block-size", "128", "--negotiate-profile", "--radio-profiles", filepath.Join(dir, "profiles.json")}
	if opts.PrepareRoute {
		relayRF := cfg.RelayRecipient
		if relayRF == "" {
			relayRF = cfg.Via
		}
		args = append(args, "--prepare-route", "--recipient", target, "--relay-recipient", relayRF)
	}
	if opts.PreflightOnly {
		args = append(args, "--preflight-only")
	}
	args = append(args, "--board-id", fmt.Sprintf("0x%08X", board.ID))
	if opts.ReplaceExisting {
		args = append(args, "--replace-existing")
	}
	if opts.RequireOffline {
		args = append(args, "--require-offline")
	}
	if opts.ExerciseConfirmTimeout {
		args = append(args, "--exercise-confirm-timeout")
	}
	if cfg.Via != "" {
		args = append(args, "--via", cfg.Via, "--route-key-file", filepath.Join(dir, "route.key"))
		if len(cfg.Chain) > 0 {
			args = append(args, "--chain", cfg.chainArg())
		}
		if cfg.TargetNormal != nil && *cfg.TargetNormal != cfg.Normal {
			args = append(args, "--relay-key-file", filepath.Join(dir, "relay.key"))
		}
	}
	sum := sha256.Sum256(image)
	j := &Job{Relay: cfg.Via, RelayRecipient: cfg.RelayRecipient, Chain: cfg.Chain, Engine: "native-go", BoardID: board.ID, TargetBoard: cfg.TargetBoard, Source: source, SignedTarget: signedTarget, ExpectedVersion: opts.ExpectedVersion, BlockSize: 128, Options: opts, ID: id, Target: target, State: "running", Phase: "connecting", Total: len(image), SHA256: hex.EncodeToString(sum[:]), CanPause: true, Started: time.Now().UTC(), dir: dir}
	s.jobs[target] = j
	s.active = id
	if e = s.persist(j); e != nil {
		s.active = ""
		delete(s.jobs, target)
		os.RemoveAll(dir)
		return nil, e
	}
	go s.run(j, runner, args)
	copy := *j
	return &copy, nil
}
func (s *Service) persist(j *Job) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	return write(filepath.Join(j.dir, "job.json"), b)
}
func (s *Service) run(j *Job, runner Runner, args []string) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { e := runner(args, writer); writer.Close(); done <- e }()
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 4096), 65536)
	// Engine output is private on disk; only selected fields reach the browser.
	log, logErr := os.OpenFile(filepath.Join(j.dir, "client-output.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if logErr == nil {
		defer log.Close()
	}
	for scan.Scan() {
		line := scan.Bytes()
		if log != nil {
			_, _ = log.Write(append(append([]byte{}, line...), '\n'))
		}
		var event struct {
			Event     string     `json:"event"`
			Op        int        `json:"op"`
			Status    int        `json:"status"`
			Written   int        `json:"written"`
			Reply     *BootProof `json:"reply"`
			Recipient string     `json:"recipient"`
			Message   string     `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		s.mu.Lock()
		oldPhase := j.Phase
		switch event.Event {
		case "rendezvous_established":
			j.Phase = "checking_route"
		case "target_service_verified":
			j.TargetServiceVerified = true
		case "relay_service_restored":
			j.RelayServiceRestored = true
		case "controller_service_restored":
			j.ControllerServiceRestored = true
		case "route_preparation_failed":
			j.Error = event.Message
		case "route_role_configured":
			j.RolesChanged = append(j.RolesChanged, event.Recipient)
		case "route_preparation_starting":
			j.Phase = "preparing_route"
		case "route_roles_observed":
			j.Phase = "checking_route"
		case "route_authenticated":
			j.RouteAuthenticated = true
			j.RouteProof = event.Reply
		case "preflight_releasing_session":
			j.Phase = "releasing_session"
		case "preflight_session_released":
			j.SessionReleased = true
		case "preflight_verified":
			j.PreflightVerified = true

		case "restoration_unconfirmed":
			j.Phase = "restoration_failed"
			j.Error = "Radio restoration could not be confirmed. Check the retained transfer log before resuming."
		case "protocol_rejected":
			if event.Status == 1 {
				switch event.Op {
				case 1, 15:
					j.Error = "Station refused the transfer opening (FORMAT). Check target board, manifest, image size and page geometry. Operation details are retained on the Graywolf host."
				case 2, 16:
					j.Error = "Station refused a firmware block (FORMAT). Check block size and offset in the retained transfer log."
				default:
					j.Error = "Station rejected an OTA operation (FORMAT). Check the operation and its parameters in the retained transfer log."
				}
			}

		case "page_committed":
			j.Written = event.Written
			j.Phase = "sending"
		case "verified":
			j.Phase = "verifying"
		case "activation_starting":
			j.Phase = "confirming"
			j.CanPause = false
		case "boot_pending_confirmation":
			j.Phase = "confirming"
			j.CanPause = false
			j.bootProof = event.Reply
		case "confirmation_withheld":
			j.Phase = "waiting_rollback"
		case "confirmation_timeout_rollback":
			j.rollbackObserved = true
			j.Phase = "reactivating"
		case "offline_before_update":
			j.offlineBefore = true
		case "offline_after_update":
			j.offlineAfter = true
		case "radio_confirmed":
			j.confirmed = validBootProof(event.Reply, j) && sameBoot(event.Reply, j.bootProof)
			j.Phase = "checking_installation"
		case "post_install_verified":
			j.PostInstallVerified = j.confirmed && validBootProof(event.Reply, j) && sameBoot(event.Reply, j.bootProof)
			if j.PostInstallVerified {
				j.Final = event.Reply
			}
		case "operator_paused":
			j.Phase = "paused"
		case "link_lost", "waiting_for_profile_recovery":
			j.Phase = "recovering"
		case "repair_page":
			j.Phase = "repairing"
		}
		if oldPhase != j.Phase || event.Event == "page_committed" || strings.HasPrefix(event.Event, "preflight_") || strings.HasPrefix(event.Event, "route_") {
			_ = s.persist(j)
		}
		s.mu.Unlock()
	}
	reader.Close()
	err := <-done
	s.mu.Lock()
	defer s.mu.Unlock()
	j.CanPause = false
	if err == nil && j.Options.PreflightOnly && j.RouteAuthenticated && j.SessionReleased && j.PreflightVerified {
		j.State = "ready"
		j.Phase = "ready"
	} else if err == nil && j.confirmed && j.PostInstallVerified && (!j.Options.RequireOffline || j.offlineBefore && j.offlineAfter) && (!j.Options.ExerciseConfirmTimeout || j.rollbackObserved) {
		j.State = "succeeded"
		j.Phase = "confirmed"
		j.Written = j.Total
	} else if j.Phase == "paused" {
		j.State = "paused"
	} else {
		j.State = "failed"
		j.Phase = "failed"
		if j.Error == "" {
			j.Error = "Update not completed. Transfer details are retained on the Graywolf host."
		}
		if j.confirmed {
			j.Error = "The new firmware was confirmed over radio, but final engine checks did not complete."
		}
	}
	if scan.Err() != nil {
		j.State = "failed"
		j.Error = "OTA engine output could not be read"
	}
	_ = s.persist(j)
	s.active = ""
	_ = os.Remove(filepath.Join(j.dir, "target.key"))
	_ = os.Remove(filepath.Join(j.dir, "route.key"))
}

func validBootProof(p *BootProof, j *Job) bool {
	return p != nil && p.Pending != nil && !*p.Pending && p.Running != 0 && p.Running == p.Boot && p.Version == j.ExpectedVersion && p.ImageSHA == j.SHA256
}
func sameBoot(p, boot *BootProof) bool {
	return p != nil && boot != nil && boot.Pending != nil && *boot.Pending && p.Running == boot.Running && p.Boot == boot.Boot && p.Version == boot.Version && p.ImageSHA == boot.ImageSHA
}

func (s *Service) SetCommandSigner(signer CommandSigner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signer = signer
}
func (s *Service) OwnsTarget(target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == s.active {
			names := []string{j.Source, j.Target, j.SignedTarget, j.Relay, j.RelayRecipient}
			for _, h := range j.Chain {
				names = append(names, h.Callsign, h.Recipient)
			}
			for _, name := range names {
				if name != "" && strings.EqualFold(name, target) {
					return true
				}
			}
		}
	}
	return false
}

// keyFingerprint is remoteactions.KeyFingerprint for the route key: first
// 3 bytes of SHA-256 over the 32 decoded bytes, 6 hex digits, "" if invalid.
func keyFingerprint(key string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(decoded) != 32 {
		return ""
	}
	sum := sha256.Sum256(decoded)
	return hex.EncodeToString(sum[:3])
}
