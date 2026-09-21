// Package rxttelemetry consumes LoRa APRS reception metadata and keeps a
// short-lived snapshot of decoded radio links. It supports both the versioned
// NDJSON event stream and the legacy /rxt.json polling endpoint.
package rxttelemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/tnc2"
)

const (
	DefaultPollInterval = 5 * time.Second
	DefaultLinkTTL      = 30 * time.Minute
	responseHeaderWait  = 4 * time.Second
	maxResponseBytes    = 1 << 20
	maxRecordBytes      = 64 << 10
	reconnectDelay      = time.Second
)

type Hop struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	HasData bool    `json:"has_data"`
	RSSI    float64 `json:"rssi_dbm"`
	SNR     float64 `json:"snr_db"`
	FO      int     `json:"fo_hz"`
	TTH     int     `json:"tth_ms,omitempty"`
}

type record struct {
	RXTime  string `json:"rx_time"`
	AgeMS   int64  `json:"age_ms"`
	Packet  string `json:"packet"`
	RXTHops []Hop  `json:"rxt_hops"`
}

type streamAddress struct {
	Text     string `json:"text"`
	Repeated bool   `json:"repeated"`
	Kind     string `json:"kind"`
}

// LocalMetrics is the measurement made by the JSON producer's local radio.
type LocalMetrics struct {
	RSSI float64 `json:"rssi_dbm"`
	SNR  float64 `json:"snr_db"`
	FO   float64 `json:"frequency_error_hz"`
}

// RadioParameters records the RF settings associated with a reception.
type RadioParameters struct {
	FrequencyHz     uint64 `json:"frequency_hz,omitempty"`
	BandwidthHz     uint64 `json:"bandwidth_hz,omitempty"`
	SpreadingFactor uint8  `json:"spreading_factor,omitempty"`
	CodingRate      string `json:"coding_rate,omitempty"`
}

// RXTHop preserves one protocol RXT hop. Pointer metrics retain the
// distinction between an explicit zero and the absence required by
// has_data:false.
type RXTHop struct {
	Ordinal        uint64   `json:"ordinal"`
	TX             string   `json:"tx,omitempty"`
	RX             string   `json:"rx,omitempty"`
	IdentityStatus string   `json:"identity_status,omitempty"`
	HasData        bool     `json:"has_data"`
	RSSI           *float64 `json:"rssi_dbm,omitempty"`
	SNR            *float64 `json:"snr_db,omitempty"`
	FO             *int     `json:"frequency_error_hz,omitempty"`
	TTH            *int     `json:"tth_ms,omitempty"`
	RSSIClipped    bool     `json:"rssi_clipped,omitempty"`
	SNRClipped     bool     `json:"snr_clipped,omitempty"`
	FOClipped      bool     `json:"frequency_error_clipped,omitempty"`
}

type RXTTelemetry struct {
	Encoding string   `json:"encoding"`
	Raw      string   `json:"raw"`
	Hops     []RXTHop `json:"hops"`
}

type streamRecord struct {
	Protocol        string `json:"protocol"`
	ProtocolVersion string `json:"protocol_version"`
	SchemaVersion   string `json:"schema_version"`
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	BootID          string `json:"boot_id"`
	Sequence        uint64 `json:"sequence"`
	CreatedAt       string `json:"created_at"`
	UptimeMS        uint64 `json:"uptime_ms"`
	RequestedAfter  string `json:"requested_after"`
	Reason          string `json:"reason"`
	rawJSON         []byte
	Capabilities    struct {
		Features []string `json:"features"`
	} `json:"capabilities"`
	Receiver struct {
		Station   string `json:"station"`
		Interface string `json:"interface"`
	} `json:"receiver"`
	Packet struct {
		RawTNC2     []byte          `json:"raw_tnc2_base64"`
		RFTNC2      []byte          `json:"rf_tnc2_base64"`
		TNC2        string          `json:"tnc2"`
		ParseStatus string          `json:"parse_status"`
		Source      streamAddress   `json:"source"`
		Destination streamAddress   `json:"destination"`
		Path        []streamAddress `json:"path"`
		Information struct {
			Raw []byte `json:"raw_base64"`
		} `json:"information"`
	} `json:"packet"`
	Reception struct {
		Local    *LocalMetrics    `json:"local"`
		Radio    *RadioParameters `json:"radio"`
		RXT      *RXTTelemetry    `json:"rxt"`
		CRCValid bool             `json:"crc_valid"`
	} `json:"reception"`
}

// RawReception is the universal, lossless protocol-level container delivered
// for every accepted rx event. Packet is present only when parse_status is
// parsed (or for a compatible legacy record whose envelope can be parsed).
type RawReception struct {
	RawEvent          []byte           `json:"raw_event,omitempty"`
	EventID           string           `json:"event_id"`
	BootID            string           `json:"boot_id"`
	Sequence          uint64           `json:"sequence"`
	CreatedAt         string           `json:"created_at,omitempty"`
	UptimeMS          uint64           `json:"uptime_ms"`
	Receiver          string           `json:"receiver"`
	ReceiverInterface string           `json:"receiver_interface,omitempty"`
	RawTNC2           []byte           `json:"raw_tnc2"`
	RFTNC2            []byte           `json:"rf_tnc2,omitempty"`
	TNC2Text          string           `json:"tnc2,omitempty"`
	ParseStatus       string           `json:"parse_status"`
	Packet            *tnc2.TNC2Packet `json:"packet,omitempty"`
	Local             *LocalMetrics    `json:"local,omitempty"`
	Radio             *RadioParameters `json:"radio,omitempty"`
	RXT               *RXTTelemetry    `json:"rxt,omitempty"`
	CRCValid          bool             `json:"crc_valid"`
	Warnings          []string         `json:"warnings,omitempty"`
}

type PacketHandler func(context.Context, RawReception) error

type ResumeState struct {
	Endpoint  string
	EventID   string
	BootID    string
	Supported bool
}

type ResumeHandler func(ResumeState) error

type Link struct {
	Hop
	ObservedAt time.Time `json:"observed_at"`
	Packet     string    `json:"packet,omitempty"`
}

// Status describes the health of the configured RXT source.
type Status struct {
	Enabled         bool       `json:"enabled"`
	Endpoint        string     `json:"endpoint"`
	LastAttemptAt   *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	RecordsReceived int        `json:"records_received"`
	ActiveLinks     int        `json:"active_links"`
	ResumeSupported bool       `json:"resume_supported"`
	LastEventID     string     `json:"last_event_id,omitempty"`
}

type Service struct {
	endpoint string
	client   *http.Client
	poll     time.Duration
	retry    time.Duration
	ttl      time.Duration
	logger   *slog.Logger
	changed  chan struct{}

	mu            sync.RWMutex
	links         map[string]Link
	handler       PacketHandler
	resumeHandler ResumeHandler

	lastAttempt     time.Time
	lastSuccess     time.Time
	lastError       string
	recordsReceived int
	lastBootID      string
	lastSequence    uint64
	lastEventID     string
	resumeSupported bool
}

func New(endpoint string, client *http.Client, logger *slog.Logger) *Service {
	if client == nil {
		// Bound connection/header setup without applying a whole-request
		// timeout: a healthy streaming response is expected to stay open.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = responseHeaderWait
		client = &http.Client{Transport: transport}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		endpoint: strings.TrimSpace(endpoint), client: client,
		poll: DefaultPollInterval, retry: reconnectDelay, ttl: DefaultLinkTTL,
		logger: logger.With("component", "rxttelemetry"), links: make(map[string]Link),
		changed: make(chan struct{}, 1),
	}
}

func (s *Service) Endpoint() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endpoint
}

func (s *Service) SetEndpoint(endpoint string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == s.endpoint {
		s.mu.Unlock()
		return
	}
	s.endpoint = endpoint
	s.links = make(map[string]Link)
	s.lastAttempt = time.Time{}
	s.lastSuccess = time.Time{}
	s.lastError = ""
	s.recordsReceived = 0
	s.lastBootID = ""
	s.lastSequence = 0
	s.lastEventID = ""
	s.resumeSupported = false
	resumeHandler := s.resumeHandler
	state := s.resumeStateLocked()
	s.mu.Unlock()
	if resumeHandler != nil {
		if err := resumeHandler(state); err != nil {
			s.logger.Warn("persist reset RXT resume cursor", "err", err)
		}
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Service) SetResumeState(eventID, bootID string, supported bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.lastEventID = eventID
	s.lastBootID = bootID
	s.resumeSupported = supported
	s.mu.Unlock()
}

func (s *Service) SetResumeHandler(handler ResumeHandler) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.resumeHandler = handler
	s.mu.Unlock()
}

func (s *Service) SetPacketHandler(handler PacketHandler) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.handler = handler
	s.mu.Unlock()
}

func (s *Service) Enabled() bool { return s != nil && s.Endpoint() != "" }

// Run reconnects continuous streams and also wakes immediately when the URL
// is changed in Settings. It stays alive while disabled so adding an endpoint
// at runtime does not require restarting Graywolf.
func (s *Service) Run(ctx context.Context) {
	for {
		endpoint := s.Endpoint()
		if endpoint == "" {
			select {
			case <-ctx.Done():
				return
			case <-s.changed:
				continue
			}
		}

		attemptCtx, cancel := context.WithCancel(ctx)
		type result struct {
			stream bool
			err    error
		}
		done := make(chan result, 1)
		go func() {
			stream, err := s.consumeOnce(attemptCtx, endpoint)
			done <- result{stream: stream, err: err}
		}()

		var res result
		select {
		case <-ctx.Done():
			cancel()
			<-done
			return
		case <-s.changed:
			cancel()
			<-done
			continue
		case res = <-done:
			cancel()
		}
		if res.err != nil && !errors.Is(res.err, context.Canceled) {
			s.setPollError(res.err.Error())
			if ctx.Err() == nil {
				s.logger.Debug("RXT source disconnected", "endpoint", endpoint, "err", res.err)
			}
		}

		delay := s.poll
		if res.stream || res.err != nil {
			delay = s.retry
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Service) Snapshot(now time.Time) []Link {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Link, 0, len(s.links))
	for key, link := range s.links {
		if now.Sub(link.ObservedAt) > s.ttl {
			delete(s.links, key)
			continue
		}
		out = append(out, link)
	}
	return out
}

func (s *Service) Status(now time.Time) Status {
	if s == nil {
		return Status{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, link := range s.links {
		if now.Sub(link.ObservedAt) > s.ttl {
			delete(s.links, key)
		}
	}
	status := Status{
		Enabled: s.endpoint != "", Endpoint: s.endpoint, LastError: s.lastError,
		RecordsReceived: s.recordsReceived, ActiveLinks: len(s.links),
		ResumeSupported: s.resumeSupported, LastEventID: s.lastEventID,
	}
	if !s.lastAttempt.IsZero() {
		attempt := s.lastAttempt
		status.LastAttemptAt = &attempt
	}
	if !s.lastSuccess.IsZero() {
		success := s.lastSuccess
		status.LastSuccessAt = &success
	}
	return status
}

func (s *Service) setPollError(message string) {
	s.mu.Lock()
	s.lastError = message
	s.mu.Unlock()
}

// pollOnce remains the focused test seam for the legacy endpoint.
func (s *Service) pollOnce(ctx context.Context) {
	_, err := s.consumeOnce(ctx, s.Endpoint())
	if err != nil && !errors.Is(err, context.Canceled) {
		s.setPollError(err.Error())
	}
}

// consumeOnce performs one legacy poll or consumes one NDJSON connection
// until it closes. The bool reports whether the response was a stream.
func (s *Service) consumeOnce(ctx context.Context, endpoint string) (bool, error) {
	if endpoint == "" {
		return false, nil
	}
	s.mu.Lock()
	s.lastAttempt = time.Now().UTC()
	s.mu.Unlock()
	requestURL, resumed, err := s.resumeRequestURL(endpoint)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/x-ndjson, application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusBadRequest && resumed {
			s.clearResumeState()
		}
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "application/x-ndjson" || mediaType == "application/ndjson" {
		return true, s.consumeStream(ctx, resp.Body)
	}
	return false, s.consumeLegacy(resp.Body)
}

func (s *Service) resumeURL(endpoint string) (string, error) {
	requestURL, _, err := s.resumeRequestURL(endpoint)
	return requestURL, err
}

func (s *Service) resumeRequestURL(endpoint string) (string, bool, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, err
	}
	s.mu.RLock()
	eventID := s.lastEventID
	supported := s.resumeSupported
	s.mu.RUnlock()
	if supported && eventID != "" {
		query := u.Query()
		query.Set("after", eventID)
		u.RawQuery = query.Encode()
		return u.String(), true, nil
	}
	return u.String(), false, nil
}

func (s *Service) clearResumeState() {
	s.mu.Lock()
	s.lastEventID = ""
	s.lastBootID = ""
	s.lastSequence = 0
	s.resumeSupported = false
	resumeHandler := s.resumeHandler
	state := s.resumeStateLocked()
	s.mu.Unlock()
	s.persistResumeState(resumeHandler, state)
}

func (s *Service) consumeLegacy(body io.Reader) error {
	var rows []record
	if err := json.NewDecoder(io.LimitReader(body, maxResponseBytes)).Decode(&rows); err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSuccess = now
	s.lastError = ""
	s.recordsReceived = len(rows)
	for _, row := range rows {
		observed := now.Add(-time.Duration(max(row.AgeMS, 0)) * time.Millisecond)
		for _, hop := range row.RXTHops {
			s.storeLinkLocked(hop, observed, row.Packet)
		}
	}
	return nil
}

func (s *Service) consumeStream(ctx context.Context, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), maxRecordBytes)
	for scanner.Scan() {
		var event streamRecord
		rawRecord := append([]byte(nil), scanner.Bytes()...)
		if err := json.Unmarshal(rawRecord, &event); err != nil {
			return fmt.Errorf("decode NDJSON record: %w", err)
		}
		event.rawJSON = rawRecord
		if err := s.processStreamRecord(ctx, event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return io.ErrUnexpectedEOF
}

func (s *Service) processStreamRecord(ctx context.Context, event streamRecord) error {
	if event.Protocol != "lora-aprs-json" || event.ProtocolVersion != "1" {
		return fmt.Errorf("unsupported protocol %q version %q", event.Protocol, event.ProtocolVersion)
	}
	if event.SchemaVersion != "" && event.SchemaVersion != "1.0" {
		return fmt.Errorf("unsupported schema version %q", event.SchemaVersion)
	}
	now := time.Now().UTC()
	if event.Event == "hello" {
		s.mu.Lock()
		s.lastSuccess = now
		s.lastError = ""
		resumeSupported := contains(event.Capabilities.Features, "history_resume")
		if event.BootID != s.lastBootID {
			s.lastBootID = event.BootID
			s.lastSequence = 0
			s.lastEventID = ""
		}
		if !resumeSupported {
			s.lastEventID = ""
		}
		s.resumeSupported = resumeSupported
		resumeHandler := s.resumeHandler
		state := s.resumeStateLocked()
		s.mu.Unlock()
		s.persistResumeState(resumeHandler, state)
		return nil
	}
	if event.Event == "heartbeat" {
		s.mu.Lock()
		s.lastSuccess = now
		s.lastError = ""
		s.mu.Unlock()
		return nil
	}
	if event.Event == "gap" {
		s.mu.Lock()
		s.lastSuccess = now
		s.lastError = ""
		s.lastEventID = ""
		s.lastSequence = 0
		resumeHandler := s.resumeHandler
		state := s.resumeStateLocked()
		s.mu.Unlock()
		s.logger.Warn("RXT history gap", "requested_after", event.RequestedAfter, "reason", event.Reason)
		s.persistResumeState(resumeHandler, state)
		return nil
	}
	if event.Event != "rx" {
		return nil
	}
	if len(event.Packet.RawTNC2) == 0 {
		return errors.New("rx record has no raw_tnc2_base64 payload")
	}
	if event.EventID == "" || event.BootID == "" || event.Sequence == 0 {
		return errors.New("rx record has incomplete identity")
	}
	reception := RawReception{
		RawEvent:          append([]byte(nil), event.rawJSON...),
		EventID:           event.EventID,
		BootID:            event.BootID,
		Sequence:          event.Sequence,
		CreatedAt:         event.CreatedAt,
		UptimeMS:          event.UptimeMS,
		Receiver:          event.Receiver.Station,
		ReceiverInterface: event.Receiver.Interface,
		RawTNC2:           append([]byte(nil), event.Packet.RawTNC2...),
		RFTNC2:            append([]byte(nil), event.Packet.RFTNC2...),
		TNC2Text:          event.Packet.TNC2,
		ParseStatus:       event.Packet.ParseStatus,
		Local:             event.Reception.Local,
		Radio:             event.Reception.Radio,
		RXT:               event.Reception.RXT,
		CRCValid:          event.Reception.CRCValid,
	}
	if event.Packet.TNC2 != "" && event.Packet.TNC2 != string(event.Packet.RawTNC2) {
		reception.Warnings = append(reception.Warnings,
			"packet.tnc2 differs from authoritative raw_tnc2_base64")
	}
	switch event.Packet.ParseStatus {
	case "parsed":
		packet, err := tnc2.Parse(event.Packet.RawTNC2)
		if err != nil {
			reception.Warnings = append(reception.Warnings,
				"could not derive TNC2 packet: "+err.Error())
		} else {
			reception.Packet = packet
		}
	case "malformed":
		// A malformed protocol reception is still valid and lossless. It
		// deliberately has no derived TNC2Packet.
	case "":
		// Compatibility with pre-1.0 pilot producers. Current schema 1.0
		// requires parse_status, but old streams are harmless to retain.
		reception.Packet, _ = tnc2.Parse(event.Packet.RawTNC2)
	default:
		return fmt.Errorf("rx record has unsupported parse_status %q", event.Packet.ParseStatus)
	}

	s.mu.Lock()
	if event.BootID == s.lastBootID && s.lastSequence > 0 &&
		event.Sequence > s.lastSequence+1 && s.resumeSupported && s.lastEventID != "" {
		lastEventID := s.lastEventID
		s.mu.Unlock()
		return fmt.Errorf("rx sequence gap after %s: got %d", lastEventID, event.Sequence)
	}
	if event.BootID == s.lastBootID && event.Sequence <= s.lastSequence {
		s.mu.Unlock()
		return nil
	}
	handler := s.handler
	endpoint := s.endpoint
	s.mu.Unlock()

	// Delivery is the preservation boundary. A failure reconnects without
	// advancing the resume cursor, so this event can be replayed instead of
	// being silently skipped by a later sequence.
	if handler != nil {
		if err := handler(ctx, reception); err != nil {
			return fmt.Errorf("preserve rx record %s: %w", event.EventID, err)
		}
	}

	s.mu.Lock()
	// Another delivery or SetEndpoint may have changed state while the
	// preservation handler ran. Never attach this event to another producer.
	if s.endpoint != endpoint {
		s.mu.Unlock()
		return nil
	}
	if event.BootID == s.lastBootID && event.Sequence <= s.lastSequence {
		s.mu.Unlock()
		return nil
	}
	if event.BootID != s.lastBootID {
		s.lastBootID = event.BootID
		s.lastSequence = 0
	}
	s.lastSequence = event.Sequence
	s.lastSuccess = now
	s.lastError = ""
	s.recordsReceived++
	packetText := string(event.Packet.RawTNC2)
	if event.Reception.RXT != nil {
		for _, hop := range event.Reception.RXT.Hops {
			var rssi, snr float64
			var fo, tth int
			if hop.RSSI != nil {
				rssi = *hop.RSSI
			}
			if hop.SNR != nil {
				snr = *hop.SNR
			}
			if hop.FO != nil {
				fo = *hop.FO
			}
			if hop.TTH != nil {
				tth = *hop.TTH
			}
			s.storeLinkLocked(Hop{
				From: hop.TX, To: hop.RX, HasData: hop.HasData,
				RSSI: rssi, SNR: snr, FO: fo, TTH: tth,
			}, now, packetText)
		}
	}
	if event.Reception.Local != nil && reception.Packet != nil {
		from := reception.Packet.Source.Text
		for _, address := range reception.Packet.Path {
			if address.Repeated {
				from = strings.TrimSuffix(address.Text, "*")
			}
		}
		s.storeLinkLocked(Hop{
			From: from, To: event.Receiver.Station, HasData: true,
			RSSI: event.Reception.Local.RSSI, SNR: event.Reception.Local.SNR,
			FO: int(event.Reception.Local.FO),
		}, now, packetText)
	}
	s.lastEventID = event.EventID
	resumeHandler := s.resumeHandler
	state := s.resumeStateLocked()
	s.mu.Unlock()
	s.persistResumeState(resumeHandler, state)
	return nil
}

func (s *Service) resumeStateLocked() ResumeState {
	return ResumeState{
		Endpoint: s.endpoint, EventID: s.lastEventID,
		BootID: s.lastBootID, Supported: s.resumeSupported,
	}
}

func (s *Service) persistResumeState(handler ResumeHandler, state ResumeState) {
	if handler == nil {
		return
	}
	if err := handler(state); err != nil {
		s.logger.Warn("persist RXT resume cursor", "err", err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Service) storeLinkLocked(hop Hop, observed time.Time, packet string) {
	hop.From = strings.ToUpper(strings.TrimSpace(hop.From))
	hop.To = strings.ToUpper(strings.TrimSpace(hop.To))
	if !hop.HasData || hop.From == "" || hop.To == "" || hop.From == hop.To ||
		hop.From == "UNKNOWN" || hop.To == "UNKNOWN" {
		return
	}
	key := fmt.Sprintf("%s>%s", hop.From, hop.To)
	if old, ok := s.links[key]; ok && !observed.After(old.ObservedAt) {
		return
	}
	s.links[key] = Link{Hop: hop, ObservedAt: observed, Packet: packet}
}
