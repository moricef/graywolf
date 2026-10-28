// Package rfdx tracks positioned stations heard directly at the RF ingress.
// It deliberately does not consult the mixed RF/APRS-IS station cache.
package rfdx

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/gps"
)

const Window = time.Hour

// HeardWindow follows the APRSISCE/32 DIR_CNT/RF_CNT convention.
const HeardWindow = 30 * time.Minute

type Result struct {
	Channel    uint32    `json:"channel"`
	Callsign   string    `json:"callsign"`
	DistanceKM float64   `json:"distance_km"`
	BearingDeg float64   `json:"bearing_deg"`
	ReceivedAt time.Time `json:"received_at"`
}

type Snapshot struct {
	MaxEver            []Result            `json:"max_ever"`
	WindowSeconds      int                 `json:"window_seconds"`
	HeardWindowSeconds int                 `json:"heard_window_seconds"`
	PositionAvailable  bool                `json:"position_available"`
	Channels           []Result            `json:"channels"`
	Statistics         []ChannelStatistics `json:"statistics"`
}

// ChannelStatistics counts distinct source callsigns, never packet volume.
// LOC_CNT follows Graywolf's current direct-only message reachability rule.
type ChannelStatistics struct {
	Channel      uint32 `json:"channel"`
	MessageCount uint64 `json:"msg_cnt"`
	LocalCount   int    `json:"loc_cnt"`
	DirectCount  int    `json:"dir_cnt"`
	RFCount      int    `json:"rf_cnt"`
}

type heard struct{ lastRF, lastDirect time.Time }

type key struct {
	channel uint32
	call    string
}
type observation struct {
	Result
	lat, lon float64
}

type Tracker struct {
	mu               sync.Mutex
	entries          map[key][]observation
	config           Config
	records          map[uint32]Result
	recordResetAt    time.Time
	notifications    map[uint32]Result
	lastNotification map[uint32]Result
	heard            map[key]heard
	messages         map[uint32]uint64
	position         gps.PositionCache
	now              func() time.Time
}

func New(position gps.PositionCache) *Tracker {
	return &Tracker{entries: make(map[key][]observation), heard: make(map[key]heard), messages: make(map[uint32]uint64), position: position, now: time.Now,
		config: DefaultConfig(), records: make(map[uint32]Result), notifications: make(map[uint32]Result), lastNotification: make(map[uint32]Result)}
}

func (t *Tracker) Configure(c Config) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.config = c
	t.notifications = make(map[uint32]Result)
}

func (t *Tracker) RestoreRecords(records []Result) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range records {
		t.records[r.Channel] = r
	}
}

func (t *Tracker) ClearRecords() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = make(map[uint32]Result)
	t.recordResetAt = t.now()
}

func (t *Tracker) TakeNotifications() []Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Result
	for _, r := range t.notifications {
		out = append(out, r)
	}
	t.notifications = make(map[uint32]Result)
	return out
}

func validPosition(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && math.Abs(lat) <= 90 && math.Abs(lon) <= 180
}

// Record counts RF source stations on a known channel; only directly heard
// source positions qualify for DX. Objects/items cannot supply that position.
// The receipt clock, never the remote APRS timestamp, defines the window.
func (t *Tracker) Record(p *aprs.DecodedAPRSPacket) {
	if t == nil || p == nil || p.Direction != aprs.DirectionRF || p.Channel <= 0 || strings.TrimSpace(p.Source) == "" {
		return
	}
	direct := true
	for _, hop := range p.Path {
		hop = strings.ToUpper(strings.TrimSpace(hop))
		if hop == "TCPIP" || hop == "TCPIP*" || hop == "TCPXX" || hop == "TCPXX*" || strings.HasPrefix(hop, "QA") {
			return
		}
		if strings.Contains(hop, "*") {
			direct = false
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.prune(now)
	call := strings.ToUpper(strings.TrimSpace(p.Source))
	k := key{uint32(p.Channel), call}
	h := t.heard[k]
	h.lastRF = now
	if direct {
		h.lastDirect = now
	}
	t.heard[k] = h
	if !direct || p.Position == nil || p.ThirdParty != nil || p.Object != nil || p.Item != nil {
		return
	}
	lat, lon := p.Position.Latitude, p.Position.Longitude
	if !validPosition(lat, lon) || (lat == 0 && lon == 0) || t.config.ignored(call) {
		return
	}
	r := Result{Channel: uint32(p.Channel), Callsign: call, ReceivedAt: now}
	if t.position != nil {
		if fix, ok := t.position.Get(); ok && validPosition(fix.Latitude, fix.Longitude) {
			r.DistanceKM, r.BearingDeg = distanceBearing(fix.Latitude, fix.Longitude, lat, lon)
		}
	}
	previous := t.bestLocked(r.Channel, now)
	// Keep older/farther receptions until their own window expires. A
	// mobile moving closer must not erase its earlier maximum.
	list := t.entries[k][:0]
	for _, old := range t.entries[k] {
		if old.DistanceKM == 0 || r.DistanceKM == 0 || old.DistanceKM > r.DistanceKM {
			list = append(list, old)
		}
	}
	t.entries[k] = append(list, observation{Result: r, lat: lat, lon: lon})
	if r.DistanceKM > t.config.MinDistanceKM {
		if old, ok := t.records[r.Channel]; !ok || r.DistanceKM > old.DistanceKM {
			t.records[r.Channel] = r
		}
		last := t.lastNotification[r.Channel]
		elapsed := last.ReceivedAt.IsZero() || now.Sub(last.ReceivedAt) >= time.Duration(t.config.MinIntervalMinutes)*time.Minute
		farther := previous == nil || r.DistanceKM > previous.DistanceKM
		if t.config.NotifyEnabled && t.config.StatusEnabled && t.config.IncludeDX && t.config.Channel == r.Channel && r.DistanceKM > t.config.MinTriggerKM && (farther || (previous != nil && r.DistanceKM == previous.DistanceKM)) && (elapsed || (farther && previous != nil && previous.Callsign != r.Callsign)) {
			t.notifications[r.Channel] = r
			t.lastNotification[r.Channel] = r
		}
	}
}

func distanceBearing(lat, lon, toLat, toLon float64) (float64, float64) {
	d := aprs.HaversineDistanceMi(lat, lon, toLat, toLon) * 1.609344
	if math.IsNaN(d) {
		d = math.Pi * 6371.0088
	}
	lat1, lat2 := lat*math.Pi/180, toLat*math.Pi/180
	dlon := (toLon - lon) * math.Pi / 180
	b := math.Mod(math.Atan2(math.Sin(dlon)*math.Cos(lat2), math.Cos(lat1)*math.Sin(lat2)-math.Sin(lat1)*math.Cos(lat2)*math.Cos(dlon))*180/math.Pi+360, 360)
	return d, b
}

func (t *Tracker) bestLocked(channel uint32, now time.Time) *Result {
	var best *Result
	for _, list := range t.entries {
		for _, v := range list {
			if v.Channel != channel || !v.ReceivedAt.After(now.Add(-time.Duration(t.config.WindowMinutes)*time.Minute)) || v.DistanceKM <= t.config.MinDistanceKM || t.config.ignored(v.Callsign) {
				continue
			}
			if best == nil || v.DistanceKM > best.DistanceKM || (v.DistanceKM == best.DistanceKM && v.ReceivedAt.After(best.ReceivedAt)) {
				r := v.Result
				best = &r
			}
		}
	}
	return best
}

// RecordMessageDownlink is called only after the governor's downstream RF
// sender accepts an APRS-IS message, not when merely queued or filtered.
// It is a process-lifetime per-channel count, not an hourly count or RF ACK.
func (t *Tracker) RecordMessageDownlink(channel uint32) {
	if t == nil || channel == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.messages[channel]++
}

func (t *Tracker) prune(now time.Time) {
	for k, v := range t.heard {
		if !v.lastRF.After(now.Add(-HeardWindow)) {
			delete(t.heard, k)
		}
	}
	for k, list := range t.entries {
		kept := list[:0]
		for _, v := range list {
			if v.ReceivedAt.After(now.Add(-time.Duration(t.config.WindowMinutes) * time.Minute)) {
				kept = append(kept, v)
			}
		}
		if len(kept) == 0 {
			delete(t.entries, k)
		} else {
			t.entries[k] = kept
		}
	}
}

func (t *Tracker) Snapshot() Snapshot {
	out := Snapshot{WindowSeconds: int(Window.Seconds()), HeardWindowSeconds: int(HeardWindow.Seconds()), Channels: []Result{}, Statistics: []ChannelStatistics{}, MaxEver: []Result{}}
	if t == nil {
		return out
	}
	var fix gps.Fix
	if t.position != nil {
		var ok bool
		fix, ok = t.position.Get()
		out.PositionAvailable = ok && validPosition(fix.Latitude, fix.Longitude)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	out.WindowSeconds = int(t.config.WindowMinutes) * 60
	t.prune(now)
	stats := make(map[uint32]ChannelStatistics)
	for ch, n := range t.messages {
		stats[ch] = ChannelStatistics{Channel: ch, MessageCount: n}
	}
	for k, h := range t.heard {
		s := stats[k.channel]
		s.Channel = k.channel
		s.RFCount++
		if h.lastDirect.After(now.Add(-HeardWindow)) {
			s.DirectCount++
			s.LocalCount++
		}
		stats[k.channel] = s
	}
	for k := range t.entries {
		if _, ok := stats[k.channel]; !ok {
			stats[k.channel] = ChannelStatistics{Channel: k.channel, MessageCount: t.messages[k.channel]}
		}
	}
	for _, s := range stats {
		out.Statistics = append(out.Statistics, s)
	}
	sort.Slice(out.Statistics, func(i, j int) bool { return out.Statistics[i].Channel < out.Statistics[j].Channel })
	if !out.PositionAvailable {
		for _, r := range t.records {
			out.MaxEver = append(out.MaxEver, r)
		}
		sort.Slice(out.MaxEver, func(i, j int) bool { return out.MaxEver[i].Channel < out.MaxEver[j].Channel })
		return out
	}
	for k, list := range t.entries {
		for i := range list {
			if list[i].DistanceKM == 0 {
				list[i].DistanceKM, list[i].BearingDeg = distanceBearing(fix.Latitude, fix.Longitude, list[i].lat, list[i].lon)
			}
		}
		t.entries[k] = list
	}
	for ch := range stats {
		if r := t.bestLocked(ch, now); r != nil {
			out.Channels = append(out.Channels, *r)
			if old, ok := t.records[ch]; r.ReceivedAt.After(t.recordResetAt) && (!ok || r.DistanceKM > old.DistanceKM) {
				t.records[ch] = *r
			}
		}
	}
	for _, r := range t.records {
		out.MaxEver = append(out.MaxEver, r)
	}
	sort.Slice(out.MaxEver, func(i, j int) bool { return out.MaxEver[i].Channel < out.MaxEver[j].Channel })
	sort.Slice(out.Channels, func(i, j int) bool { return out.Channels[i].Channel < out.Channels[j].Channel })
	return out
}

// Bulletin returns current counters for one channel, plus DX when available.
// Counters remain publishable without a local fix or a qualifying DX position.
func (t *Tracker) Bulletin(channel uint32) (string, error) {
	if channel == 0 {
		return "", fmt.Errorf("iGate statistics require a receive channel")
	}
	snapshot := t.Snapshot()
	s := ChannelStatistics{Channel: channel}
	for _, v := range snapshot.Statistics {
		if v.Channel == channel {
			s = v
			break
		}
	}
	info := fmt.Sprintf("<IGATE,MSG_CNT=%d,LOC_CNT=%d,DIR_CNT=%d,RF_CNT=%d", s.MessageCount, s.LocalCount, s.DirectCount, s.RFCount)
	for _, r := range snapshot.Channels {
		if r.Channel == channel {
			info += fmt.Sprintf(",DX=%s(%.0fkm@%03ddeg)", r.Callsign, r.DistanceKM, int(math.Round(r.BearingDeg))%360)
			break
		}
	}
	return info, nil
}
