package rfdx

import (
	"fmt"
	"math"
	"strings"
)

func (t *Tracker) Report(channel uint32, kind, text string, includeDX bool) (string, error) {
	if kind == "capabilities" {
		info, err := t.Bulletin(channel)
		if !includeDX {
			if at := strings.Index(info, ",DX="); at >= 0 {
				info = info[:at]
			}
		}
		return info, err
	}
	if kind != "status" {
		return "", fmt.Errorf("unknown report kind %q", kind)
	}
	info := ">" + strings.TrimSpace(text)
	if includeDX {
		for _, r := range t.Snapshot().Channels {
			if r.Channel == channel {
				if len(info) > 1 {
					info += " "
				}
				info += fmt.Sprintf("DX=%s(%.0fkm@%03ddeg)", r.Callsign, r.DistanceKM, int(math.Round(r.BearingDeg))%360)
				break
			}
		}
	}
	if info == ">" {
		return "", fmt.Errorf("status has no text or qualifying DX")
	}
	return info, nil
}
