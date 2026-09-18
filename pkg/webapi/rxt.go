package webapi

import (
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
)

type RXTLinkSource interface {
	Enabled() bool
	Snapshot(time.Time) []rxttelemetry.Link
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

func RegisterRXT(srv *Server, mux *http.ServeMux, source RXTLinkSource, stations StationCache) {
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
