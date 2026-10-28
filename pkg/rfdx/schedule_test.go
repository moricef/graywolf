package rfdx

import (
	"testing"
	"time"
)

func TestReportTimersIndependentAndHourly(t *testing.T) {
	c := DefaultConfig()
	c.StatusEnabled = true
	c.CapabilitiesEnabled = true
	c.StatusIntervalMinutes = 20
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := NewSchedule(c, now)
	if len(s.Due(c, now.Add(19*time.Minute))) != 0 {
		t.Fatal("early report")
	}
	if got := s.Due(c, now.Add(20*time.Minute)); len(got) != 1 || got[0] != "status" {
		t.Fatal(got)
	}
	if got := s.Due(c, now.Add(40*time.Minute)); len(got) != 1 {
		t.Fatal(got)
	}
	if got := s.Due(c, now.Add(time.Hour)); len(got) != 2 || got[1] != "capabilities" {
		t.Fatal(got)
	}
	if len(s.Due(c, now.Add(time.Hour))) != 0 {
		t.Fatal("duplicate hourly report")
	}
	c.StatusEnabled = false
	c.CapabilitiesEnabled = false
	if len(s.Due(c, now.Add(3*time.Hour))) != 0 {
		t.Fatal("disabled timers emitted reports")
	}
}
