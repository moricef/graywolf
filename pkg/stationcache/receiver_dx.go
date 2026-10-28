package stationcache

import (
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"math"
	"strings"
	"sync"
	"time"
)

// ReceiverDX is computed from measured RXT links at this receiver, rather
// than from capability reports or the Graywolf host's radio position.
type ReceiverDX struct {
	WindowMinutes  uint32            `json:"window_minutes"`
	DirectStations int               `json:"direct_stations"`
	UpdatedAt      time.Time         `json:"updated_at"`
	DX             *ReceiverDXResult `json:"dx,omitempty"`
}

type ReceiverDXResult struct {
	Callsign   string    `json:"callsign"`
	DistanceKM float64   `json:"distance_km"`
	BearingDeg float64   `json:"bearing_deg"`
	ReceivedAt time.Time `json:"received_at"`
	RSSI       float64   `json:"rssi_dbm"`
	SNR        float64   `json:"snr_db"`
	FO         int       `json:"frequency_error_hz"`
}

type receiverObservation struct {
	link   rxttelemetry.Link
	result *ReceiverDXResult
}

// ReceiverTracker retains farther observations until their own expiry, so a
// mobile's closer reception cannot prematurely erase its earlier DX.
type ReceiverTracker struct {
	mu           sync.Mutex
	store        StationStore
	config       rfdx.Config
	observations map[string][]receiverObservation
}

func NewReceiverTracker(store StationStore) *ReceiverTracker {
	return &ReceiverTracker{store: store, config: rfdx.DefaultConfig(), observations: make(map[string][]receiverObservation)}
}
func (t *ReceiverTracker) Configure(c rfdx.Config) { t.mu.Lock(); defer t.mu.Unlock(); t.config = c }

func (t *ReceiverTracker) resolve(link rxttelemetry.Link) *ReceiverDXResult {
	positions := t.store.Lookup([]string{link.From, link.To})
	from, ok := positions[link.From]
	if !ok {
		return nil
	}
	to, ok := positions[link.To]
	if !ok {
		return nil
	}
	valid := func(p LatLon) bool {
		return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lon) && !math.IsInf(p.Lat, 0) && !math.IsInf(p.Lon, 0) && math.Abs(p.Lat) <= 90 && math.Abs(p.Lon) <= 180 && (p.Lat != 0 || p.Lon != 0)
	}
	if !valid(from) || !valid(to) {
		return nil
	}
	rad := math.Pi / 180
	y := math.Sin((from.Lon-to.Lon)*rad) * math.Cos(from.Lat*rad)
	x := math.Cos(to.Lat*rad)*math.Sin(from.Lat*rad) - math.Sin(to.Lat*rad)*math.Cos(from.Lat*rad)*math.Cos((from.Lon-to.Lon)*rad)
	return &ReceiverDXResult{Callsign: link.From, DistanceKM: aprs.HaversineDistanceMi(to.Lat, to.Lon, from.Lat, from.Lon) * 1.609344, BearingDeg: math.Mod(math.Atan2(y, x)/rad+360, 360), ReceivedAt: link.ObservedAt, RSSI: link.RSSI, SNR: link.SNR, FO: link.FO}
}
func (t *ReceiverTracker) prune(now time.Time) {
	cutoff := now.Add(-time.Duration(t.config.WindowMinutes) * time.Minute)
	// Retain direct peers for the 30-minute heard window, independently of DX.
	if other := now.Add(-30 * time.Minute); other.Before(cutoff) {
		cutoff = other
	}
	for key, list := range t.observations {
		kept := list[:0]
		for _, o := range list {
			if !o.link.ObservedAt.Before(cutoff) {
				kept = append(kept, o)
			}
		}
		if len(kept) == 0 {
			delete(t.observations, key)
		} else {
			t.observations[key] = kept
		}
	}
}
func (t *ReceiverTracker) Record(links []rxttelemetry.Link, now time.Time) {
	if t == nil || t.store == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now)
	for _, link := range links {
		if !link.HasData || link.From == "" || link.To == "" || link.From == link.To || link.ObservedAt.After(now) {
			continue
		}
		key := link.From + ">" + link.To
		result := t.resolve(link)
		kept := t.observations[key][:0]
		for _, old := range t.observations[key] {
			if old.link.ObservedAt.Equal(link.ObservedAt) {
				continue
			}
			// Only newer and farther samples dominate earlier ones of the same peer.
			if result != nil && old.result != nil && !link.ObservedAt.Before(old.link.ObservedAt) && result.DistanceKM >= old.result.DistanceKM {
				continue
			}
			kept = append(kept, old)
		}
		t.observations[key] = append(kept, receiverObservation{link: link, result: result})
	}
}
func (t *ReceiverTracker) ReceiverSnapshot(now time.Time) map[string]ReceiverDX {
	out := make(map[string]ReceiverDX)
	if t == nil {
		return out
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(now)
	ignored := make(map[string]bool)
	for _, call := range strings.FieldsFunc(t.config.IgnoredCalls, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }) {
		ignored[strings.ToUpper(call)] = true
	}
	peers := make(map[string]map[string]bool)
	for key, list := range t.observations {
		for i := range list {
			o := &list[i]
			rx := o.link.To
			report := out[rx]
			report.WindowMinutes = t.config.WindowMinutes
			if o.link.ObservedAt.After(report.UpdatedAt) {
				report.UpdatedAt = o.link.ObservedAt
			}
			if !o.link.ObservedAt.Before(now.Add(-30 * time.Minute)) {
				if peers[rx] == nil {
					peers[rx] = make(map[string]bool)
				}
				peers[rx][o.link.From] = true
			}
			if o.result == nil {
				o.result = t.resolve(o.link)
			}
			r := o.result
			if r != nil && r.DistanceKM > t.config.MinDistanceKM && !ignored[strings.ToUpper(r.Callsign)] && !r.ReceivedAt.Before(now.Add(-time.Duration(t.config.WindowMinutes)*time.Minute)) {
				if report.DX == nil || r.DistanceKM > report.DX.DistanceKM || (r.DistanceKM == report.DX.DistanceKM && r.ReceivedAt.After(report.DX.ReceivedAt)) {
					copy := *r
					report.DX = &copy
				}
			}
			out[rx] = report
		}
		t.observations[key] = list
	}
	for rx, report := range out {
		report.DirectStations = len(peers[rx])
		out[rx] = report
	}
	return out
}
