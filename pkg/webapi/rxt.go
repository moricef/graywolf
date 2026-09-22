package webapi

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
)

type RXTLinkSource interface {
	Enabled() bool
	Endpoints() []string
	SetSources([]rxttelemetry.SourceConfig)
	Snapshot(time.Time) []rxttelemetry.Link
	Statuses(time.Time) []rxttelemetry.Status
}

type RXTLinkDTO struct {
	rxttelemetry.Link
	FromPosition *RXTPositionDTO `json:"from_position,omitempty"`
	ToPosition   *RXTPositionDTO `json:"to_position,omitempty"`
}

type RXTPositionDTO struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type rxtConfigDTO struct {
	Endpoints []string `json:"endpoints"`
	Endpoint  string   `json:"endpoint,omitempty"`
}

type rxtStatusDTO struct {
	rxttelemetry.Status
	Sources []rxttelemetry.Status `json:"sources"`
}

func RegisterRXT(srv *Server, mux *http.ServeMux, source RXTLinkSource, stations StationCache, store *configstore.Store) {
	mux.HandleFunc("GET /api/rxt/config", func(w http.ResponseWriter, _ *http.Request) {
		endpoints := source.Endpoints()
		var legacyEndpoint string
		if len(endpoints) > 0 {
			legacyEndpoint = endpoints[0]
		}
		writeJSON(w, http.StatusOK, rxtConfigDTO{Endpoints: endpoints, Endpoint: legacyEndpoint})
	})
	mux.HandleFunc("PUT /api/rxt/config", func(w http.ResponseWriter, r *http.Request) {
		body, err := decodeJSON[rxtConfigDTO](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		endpoints := body.Endpoints
		// Accept the original singleton request shape during upgrades.
		if len(endpoints) == 0 && strings.TrimSpace(body.Endpoint) != "" {
			endpoints = []string{body.Endpoint}
		}
		clean := make([]string, 0, len(endpoints))
		seen := make(map[string]bool, len(endpoints))
		for _, endpoint := range endpoints {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" || seen[endpoint] {
				continue
			}
			u, err := url.ParseRequestURI(endpoint)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				badRequest(w, "endpoint must be an absolute HTTP or HTTPS URL")
				return
			}
			seen[endpoint] = true
			clean = append(clean, endpoint)
		}
		if store == nil {
			http.Error(w, "RXT configuration store unavailable", http.StatusServiceUnavailable)
			return
		}
		configs, err := store.ReplaceRXTConfigs(r.Context(), clean)
		if err != nil {
			if srv != nil {
				srv.internalError(w, r, "save RXT config", err)
			} else {
				http.Error(w, err.Error(), 500)
			}
			return
		}
		sources := make([]rxttelemetry.SourceConfig, 0, len(configs))
		for _, config := range configs {
			sources = append(sources, rxttelemetry.SourceConfig{
				Endpoint: config.Endpoint, LastEventID: config.LastEventID,
				LastBootID: config.LastBootID, ResumeSupported: config.ResumeSupported,
			})
		}
		source.SetSources(sources)
		endpoints = source.Endpoints()
		var legacyEndpoint string
		if len(endpoints) > 0 {
			legacyEndpoint = endpoints[0]
		}
		writeJSON(w, http.StatusOK, rxtConfigDTO{Endpoints: endpoints, Endpoint: legacyEndpoint})
	})
	mux.HandleFunc("GET /api/rxt/status", func(w http.ResponseWriter, _ *http.Request) {
		now := time.Now().UTC()
		statuses := source.Statuses(now)
		legacy := rxttelemetry.Status{Enabled: source.Enabled()}
		if len(statuses) > 0 {
			legacy = statuses[0]
		}
		legacy.Enabled = source.Enabled()
		legacy.ActiveLinks = len(source.Snapshot(now))
		writeJSON(w, http.StatusOK, rxtStatusDTO{Status: legacy, Sources: statuses})
	})
	var lastCounts atomic.Uint64
	mux.HandleFunc("GET /api/rxt/links", func(w http.ResponseWriter, _ *http.Request) {
		if source == nil || !source.Enabled() {
			writeJSON(w, http.StatusOK, []RXTLinkDTO{})
			return
		}
		links := source.Snapshot(time.Now().UTC())
		calls := make([]string, 0, len(links)*2)
		for _, link := range links {
			calls = append(calls, link.From, link.To)
		}
		positions := stations.Lookup(calls)
		out := make([]RXTLinkDTO, 0, len(links))
		drawable := 0
		for _, link := range links {
			dto := RXTLinkDTO{Link: link}
			if p, ok := positions[link.From]; ok {
				dto.FromPosition = &RXTPositionDTO{Lat: p.Lat, Lon: p.Lon}
			}
			if p, ok := positions[link.To]; ok {
				dto.ToPosition = &RXTPositionDTO{Lat: p.Lat, Lon: p.Lon}
			}
			if dto.FromPosition != nil && dto.ToPosition != nil {
				drawable++
			}
			out = append(out, dto)
		}
		counts := uint64(len(out))<<32 | uint64(drawable)
		if srv != nil && counts != lastCounts.Swap(counts) {
			srv.logger.Info("RXT links updated", "links", len(out), "drawable", drawable)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ObservedAt.After(out[j].ObservedAt) })
		writeJSON(w, http.StatusOK, out)
	})
}
