package rfdx

import "time"

// Schedule owns independent status and capability timers; neither relies on
// position-beacon transmissions or on the DX retention window.
type Schedule struct{ statusAt, capabilitiesAt time.Time }

func NewSchedule(c Config, now time.Time) Schedule {
	return Schedule{statusAt: now.Add(time.Duration(c.StatusIntervalMinutes) * time.Minute), capabilitiesAt: now.Add(time.Duration(c.CapabilitiesIntervalMinutes) * time.Minute)}
}

func (s *Schedule) Due(c Config, now time.Time) []string {
	var out []string
	if c.StatusEnabled && !now.Before(s.statusAt) {
		out = append(out, "status")
		s.statusAt = now.Add(time.Duration(c.StatusIntervalMinutes) * time.Minute)
	}
	if c.CapabilitiesEnabled && !now.Before(s.capabilitiesAt) {
		out = append(out, "capabilities")
		s.capabilitiesAt = now.Add(time.Duration(c.CapabilitiesIntervalMinutes) * time.Minute)
	}
	return out
}
