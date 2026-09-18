// Package rxttelemetry polls the JSON side-channel exposed by LoRa APRS
// iGates and keeps a short-lived snapshot of decoded radio links.
package rxttelemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPollInterval = 5 * time.Second
	DefaultLinkTTL      = 30 * time.Minute
	maxResponseBytes    = 1 << 20
)

type Hop struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	HasData bool    `json:"has_data"`
	RSSI    float64 `json:"rssi_dbm"`
	SNR     float64 `json:"snr_db"`
	FO      int     `json:"fo_hz"`
	TTH     int     `json:"tth_ms"`
}

type record struct {
	RXTime  string `json:"rx_time"`
	AgeMS   int64  `json:"age_ms"`
	Packet  string `json:"packet"`
	RXTHops []Hop  `json:"rxt_hops"`
}

type Link struct {
	Hop
	ObservedAt time.Time `json:"observed_at"`
	Packet     string    `json:"packet,omitempty"`
}

type Service struct {
	endpoint string
	client   *http.Client
	poll     time.Duration
	ttl      time.Duration
	logger   *slog.Logger

	mu    sync.RWMutex
	links map[string]Link
}

func New(endpoint string, client *http.Client, logger *slog.Logger) *Service {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		endpoint: strings.TrimSpace(endpoint), client: client,
		poll: DefaultPollInterval, ttl: DefaultLinkTTL,
		logger: logger.With("component", "rxttelemetry"), links: make(map[string]Link),
	}
}

func (s *Service) Enabled() bool { return s != nil && s.endpoint != "" }

func (s *Service) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	s.pollOnce(ctx)
	t := time.NewTicker(s.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.pollOnce(ctx)
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

func (s *Service) pollOnce(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		s.logger.Warn("invalid RXT endpoint", "err", err)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Debug("RXT poll failed", "err", err)
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.logger.Debug("RXT poll returned non-200", "status", resp.StatusCode)
		return
	}
	var rows []record
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&rows); err != nil {
		s.logger.Debug("RXT response decode failed", "err", err)
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		// The firmware currently emits rx_time as a wall-clock-only value
		// ("16:15:01"). age_ms is unambiguous and also survives timezone
		// differences between the iGate and Graywolf, so it is canonical.
		observed := now.Add(-time.Duration(max(row.AgeMS, 0)) * time.Millisecond)
		for _, hop := range row.RXTHops {
			hop.From = strings.ToUpper(strings.TrimSpace(hop.From))
			hop.To = strings.ToUpper(strings.TrimSpace(hop.To))
			if !hop.HasData || hop.From == "" || hop.To == "" || hop.From == "UNKNOWN" || hop.To == "UNKNOWN" {
				continue
			}
			key := fmt.Sprintf("%s>%s", hop.From, hop.To)
			if old, ok := s.links[key]; ok && !observed.After(old.ObservedAt) {
				continue
			}
			s.links[key] = Link{Hop: hop, ObservedAt: observed, Packet: row.Packet}
		}
	}
}
