package rfdx

import (
	"math"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/gps"
)

func testTracker() (*Tracker, *time.Time) {
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{Latitude: 43, Longitude: 1, Timestamp: time.Now()})
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	tr := New(cache)
	tr.now = func() time.Time { return now }
	return tr, &now
}

func packet(call string, channel int, lat float64) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: call, Channel: channel, Direction: aprs.DirectionRF,
		Position: &aprs.Position{Latitude: lat, Longitude: 1}, Timestamp: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestDXDistanceBearingChannelsAndExpiry(t *testing.T) {
	tr, now := testTracker()
	tr.Record(packet("FAR", 1, 45))
	*now = now.Add(30 * time.Minute)
	tr.Record(packet("NEAR", 1, 44))
	tr.Record(packet("SOUTH", 2, 42))
	s := tr.Snapshot()
	if !s.PositionAvailable || s.WindowSeconds != 3600 || len(s.Channels) != 2 {
		t.Fatalf("snapshot: %+v", s)
	}
	if s.Channels[0].Callsign != "FAR" || math.Abs(s.Channels[0].DistanceKM-222.4) > 0.2 || s.Channels[0].BearingDeg != 0 {
		t.Fatalf("north: %+v", s.Channels[0])
	}
	if s.Channels[1].Callsign != "SOUTH" || s.Channels[1].BearingDeg != 180 {
		t.Fatalf("south: %+v", s.Channels[1])
	}
	bulletin, err := tr.Bulletin(1)
	if err != nil || bulletin != "<IGATE,MSG_CNT=0,LOC_CNT=1,DIR_CNT=1,RF_CNT=1,DX=FAR(222km@000deg)" {
		t.Fatalf("bulletin=%q err=%v", bulletin, err)
	}
	*now = now.Add(30 * time.Minute)
	s = tr.Snapshot()
	if s.Channels[0].Callsign != "NEAR" {
		t.Fatalf("expired record still wins: %+v", s)
	}
	*now = now.Add(30 * time.Minute)
	if len(tr.Snapshot().Channels) != 0 {
		t.Fatal("expired records retained")
	}
	if bulletin, err := tr.Bulletin(1); err != nil || bulletin != "<IGATE,MSG_CNT=0,LOC_CNT=0,DIR_CNT=0,RF_CNT=0" {
		t.Fatalf("empty hour must publish counters only: %q %v", bulletin, err)
	}
}

func TestDXRejectsIndirectInternetAndNonStationPositions(t *testing.T) {
	for _, kind := range []string{"digi", "alias", "is", "tcpip", "q", "thirdparty", "object", "item", "unknown-channel", "invalid", "no-position"} {
		t.Run(kind, func(t *testing.T) {
			tr, _ := testTracker()
			p := packet("INVALID", 1, 44)
			switch kind {
			case "digi":
				p.Path = []string{"F4MLV-10*", "WIDE2-1"}
			case "alias":
				p.Path = []string{"WIDE1*"}
			case "is":
				p.Direction = aprs.DirectionIS
			case "tcpip":
				p.Path = []string{"TCPIP"}
			case "q":
				p.Path = []string{"qAR", "IGATE"}
			case "thirdparty":
				p.ThirdParty = &aprs.DecodedAPRSPacket{}
			case "object":
				p.Object = &aprs.Object{}
			case "item":
				p.Item = &aprs.Item{}
			case "unknown-channel":
				p.Channel = 0
			case "invalid":
				p.Position.Latitude = math.NaN()
			case "no-position":
				p.Position = nil
			}
			tr.Record(p)
			if len(tr.Snapshot().Channels) != 0 {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
	tr, _ := testTracker()
	p := packet("DIRECT", 1, 44)
	p.Path = []string{"WIDE1-1", "WIDE2-1"}
	tr.Record(p)
	if len(tr.Snapshot().Channels) != 1 {
		t.Fatal("unused path aliases should allow direct reception")
	}
}

func TestDXKeepsFartherReceptionUntilItsWindowExpires(t *testing.T) {
	tr, now := testTracker()
	tr.Record(packet("MOBILE", 1, 45))
	*now = now.Add(time.Minute)
	tr.Record(packet("MOBILE", 1, 44))
	if tr.Snapshot().Channels[0].DistanceKM < 222 {
		t.Fatal("closer mobile reception erased a still-valid farther reception")
	}
	*now = now.Add(59 * time.Minute)
	if tr.Snapshot().Channels[0].DistanceKM > 112 {
		t.Fatal("expired farther reception retained")
	}
	tr.position = gps.NewMemCache()
	if s := tr.Snapshot(); s.PositionAvailable || len(s.Channels) != 0 {
		t.Fatalf("missing local position: %+v", s)
	}
	var absent *Tracker
	absent.Record(nil)
	if len(absent.Snapshot().Channels) != 0 {
		t.Fatal("nil tracker")
	}
}
