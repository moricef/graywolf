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
	"strings"
	"sync"
	"time"
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

type streamMetrics struct {
	RSSI float64 `json:"rssi_dbm"`
	SNR  float64 `json:"snr_db"`
	FO   int     `json:"frequency_error_hz"`
}

type streamHop struct {
	TX      string  `json:"tx"`
	RX      string  `json:"rx"`
	HasData bool    `json:"has_data"`
	RSSI    float64 `json:"rssi_dbm"`
	SNR     float64 `json:"snr_db"`
	FO      int     `json:"frequency_error_hz"`
	TTH     int     `json:"tth_ms"`
}

type streamRecord struct {
	Protocol        string `json:"protocol"`
	ProtocolVersion string `json:"protocol_version"`
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	BootID          string `json:"boot_id"`
	Sequence        uint64 `json:"sequence"`
	Receiver        struct {
		Station string `json:"station"`
	} `json:"receiver"`
	Packet struct {
		RawTNC2 []byte          `json:"raw_tnc2_base64"`
		TNC2    string          `json:"tnc2"`
		Source  streamAddress   `json:"source"`
		Path    []streamAddress `json:"path"`
	} `json:"packet"`
	Reception struct {
		Local *streamMetrics `json:"local"`
		RXT   *struct {
			Hops []streamHop `json:"hops"`
		} `json:"rxt"`
	} `json:"reception"`
}

// RXEvent is the lossless packet payload delivered to the application APRS
// pipeline after the service has accepted an rx stream record.
type RXEvent struct {
	EventID  string
	BootID   string
	Sequence uint64
	Receiver string
	TNC2     []byte
}

type PacketHandler func(context.Context, RXEvent) error

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
}

type Service struct {
	endpoint string
	client   *http.Client
	poll     time.Duration
	retry    time.Duration
	ttl      time.Duration
	logger   *slog.Logger
	changed  chan struct{}

	mu      sync.RWMutex
	links   map[string]Link
	handler PacketHandler

	lastAttempt     time.Time
	lastSuccess     time.Time
	lastError       string
	recordsReceived int
	lastBootID      string
	lastSequence    uint64
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
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "application/x-ndjson" || mediaType == "application/ndjson" {
		return true, s.consumeStream(ctx, resp.Body)
	}
	return false, s.consumeLegacy(resp.Body)
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
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode NDJSON record: %w", err)
		}
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
	now := time.Now().UTC()
	if event.Event == "hello" {
		s.mu.Lock()
		s.lastSuccess = now
		s.lastError = ""
		if event.BootID != s.lastBootID {
			s.lastBootID = event.BootID
			s.lastSequence = 0
		}
		s.mu.Unlock()
		return nil
	}
	if event.Event != "rx" {
		return nil
	}
	if len(event.Packet.RawTNC2) == 0 {
		return errors.New("rx record has no raw_tnc2_base64 payload")
	}

	s.mu.Lock()
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
	packetText := event.Packet.TNC2
	if packetText == "" {
		packetText = string(event.Packet.RawTNC2)
	}
	if event.Reception.RXT != nil {
		for _, hop := range event.Reception.RXT.Hops {
			s.storeLinkLocked(Hop{
				From: hop.TX, To: hop.RX, HasData: hop.HasData,
				RSSI: hop.RSSI, SNR: hop.SNR, FO: hop.FO, TTH: hop.TTH,
			}, now, packetText)
		}
	}
	if event.Reception.Local != nil {
		from := strings.TrimSuffix(event.Packet.Source.Text, "*")
		for _, address := range event.Packet.Path {
			if address.Repeated && address.Kind != "alias" {
				from = strings.TrimSuffix(address.Text, "*")
			}
		}
		s.storeLinkLocked(Hop{
			From: from, To: event.Receiver.Station, HasData: true,
			RSSI: event.Reception.Local.RSSI, SNR: event.Reception.Local.SNR,
			FO: event.Reception.Local.FO,
		}, now, packetText)
	}
	handler := s.handler
	s.mu.Unlock()

	if handler != nil {
		if err := handler(ctx, RXEvent{
			EventID: event.EventID, BootID: event.BootID, Sequence: event.Sequence,
			Receiver: event.Receiver.Station, TNC2: append([]byte(nil), event.Packet.RawTNC2...),
		}); err != nil {
			s.setPollError(fmt.Sprintf("deliver rx record: %v", err))
			s.logger.Debug("RXT packet delivery failed", "event_id", event.EventID, "err", err)
		}
	}
	return nil
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
