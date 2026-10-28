package rfdx

import (
	"fmt"
	"math"
	"strings"
)

// Config keeps DX detection independent of position beacons and gateways.
type Config struct {
	MinDistanceKM               float64 `json:"min_distance_km"`
	WindowMinutes               uint32  `json:"window_minutes"`
	MinTriggerKM                float64 `json:"min_trigger_km"`
	NotifyEnabled               bool    `json:"notify_enabled"`
	MinIntervalMinutes          uint32  `json:"min_interval_minutes"`
	IgnoredCalls                string  `json:"ignored_calls"`
	StatusEnabled               bool    `json:"status_enabled"`
	StatusIntervalMinutes       uint32  `json:"status_interval_minutes"`
	StatusText                  string  `json:"status_text"`
	IncludeDX                   bool    `json:"include_dx"`
	CapabilitiesEnabled         bool    `json:"capabilities_enabled"`
	CapabilitiesIntervalMinutes uint32  `json:"capabilities_interval_minutes"`
	Channel                     uint32  `json:"channel"`
	SendPath                    string  `json:"send_path"`
	Path                        string  `json:"path"`
}

func DefaultConfig() Config {
	return Config{WindowMinutes: 60, MinIntervalMinutes: 30, StatusIntervalMinutes: 60,
		CapabilitiesIntervalMinutes: 60, IncludeDX: true, SendPath: "is_only", StatusText: "Graywolf"}
}

func (c Config) Validate() error {
	for _, v := range []float64{c.MinDistanceKM, c.MinTriggerKM} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 21000 {
			return fmt.Errorf("DX distances must be between 0 and 21000 km")
		}
	}
	if c.WindowMinutes < 1 || c.WindowMinutes > 1440 || c.MinIntervalMinutes < 1 || c.MinIntervalMinutes > 1440 {
		return fmt.Errorf("DX window and minimum notification interval must be 1..1440 minutes")
	}
	if c.StatusIntervalMinutes < 1 || c.StatusIntervalMinutes > 1440 || c.CapabilitiesIntervalMinutes < 1 || c.CapabilitiesIntervalMinutes > 1440 {
		return fmt.Errorf("report intervals must be 1..1440 minutes")
	}
	if c.SendPath != "rf" && c.SendPath != "both" && c.SendPath != "is_only" {
		return fmt.Errorf("send_path must be rf, both or is_only")
	}
	if (c.StatusEnabled || c.CapabilitiesEnabled) && c.Channel == 0 {
		return fmt.Errorf("select the RF receive channel for reports")
	}
	if len(c.StatusText) > 150 || strings.ContainsAny(c.StatusText, "\r\n") {
		return fmt.Errorf("status text must be a single line of at most 150 bytes")
	}
	for _, call := range strings.FieldsFunc(c.IgnoredCalls, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		if len(call) > 32 || strings.ContainsAny(call, ":>*") {
			return fmt.Errorf("invalid ignored station %q", call)
		}
	}
	return nil
}

func (c Config) ignored(call string) bool {
	for _, v := range strings.FieldsFunc(c.IgnoredCalls, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		if strings.EqualFold(v, call) {
			return true
		}
	}
	return false
}
