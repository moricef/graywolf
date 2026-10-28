package rfdx

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestHeardCountersUniqueChannelsAndIndependentWindows(t *testing.T) {
	tr, now := testTracker()
	direct := packet("F4MLV-1", 1, 44)
	tr.Record(direct)
	tr.Record(direct) // Duplicate packet is not a second station.
	otherSSID := packet("F4MLV-2", 1, 44)
	otherSSID.Position = nil // Positionless stations still count.
	tr.Record(otherSSID)
	relayed := packet("RELAYED", 1, 46)
	relayed.Path = []string{"DIGI*", "WIDE2-1"}
	tr.Record(relayed)
	tr.Record(packet("OTHER", 2, 44))
	s := tr.Snapshot()
	if s.HeardWindowSeconds != 1800 || len(s.Statistics) != 2 || s.Statistics[0] != (ChannelStatistics{Channel: 1, LocalCount: 2, DirectCount: 2, RFCount: 3}) || s.Statistics[1].RFCount != 1 {
		t.Fatalf("counts: %+v", s)
	}
	*now = now.Add(20 * time.Minute)
	direct.Path = []string{"DIGI*"}
	tr.Record(direct) // A relay must not erase the earlier direct reception.
	if tr.Snapshot().Statistics[0].DirectCount != 2 {
		t.Fatal("relay erased direct history")
	}
	*now = now.Add(10 * time.Minute)
	s = tr.Snapshot()
	if s.Statistics[0].RFCount != 1 || s.Statistics[0].DirectCount != 0 {
		t.Fatalf("30-minute expiration: %+v", s.Statistics)
	}
	if len(s.Channels) != 2 {
		t.Fatal("heard-list expiration erased one-hour DX")
	}
	*now = now.Add(30 * time.Minute)
	if s := tr.Snapshot(); len(s.Statistics) != 0 || len(s.Channels) != 0 {
		t.Fatalf("expired data: %+v", s)
	}
}

func TestStatsWithoutPositionsAndCumulativeMessageCount(t *testing.T) {
	tr, now := testTracker()
	tr.position = nil
	p := packet("NOFIX", 1, 44)
	p.Position = nil
	tr.Record(p)
	tr.RecordMessageDownlink(1)
	tr.RecordMessageDownlink(1)
	tr.RecordMessageDownlink(2)
	tr.RecordMessageDownlink(0)
	want := "<IGATE,MSG_CNT=2,LOC_CNT=1,DIR_CNT=1,RF_CNT=1"
	if info, err := tr.Bulletin(1); err != nil || info != want {
		t.Fatalf("no position report %q %v", info, err)
	}
	*now = now.Add(2 * time.Hour)
	if info, _ := tr.Bulletin(1); info != "<IGATE,MSG_CNT=2,LOC_CNT=0,DIR_CNT=0,RF_CNT=0" {
		t.Fatal(info)
	}
	if info, _ := tr.Bulletin(2); !strings.Contains(info, "MSG_CNT=1,") {
		t.Fatal(info)
	}
	if _, err := tr.Bulletin(0); err == nil {
		t.Fatal("unknown channel allowed")
	}
}

func TestHeardCountsOuterSourceNeverObjectOrInternetIdentity(t *testing.T) {
	tr, _ := testTracker()
	p := packet("OWNER", 1, 45)
	p.Object = &aprs.Object{}
	tr.Record(p)
	p = packet("GATE", 1, 45)
	p.ThirdParty = packet("INTERNET", 1, 50)
	tr.Record(p)
	p = packet("INTERNET", 1, 50)
	p.Direction = aprs.DirectionIS
	tr.Record(p)
	s := tr.Snapshot()
	if len(s.Channels) != 0 || len(s.Statistics) != 1 || s.Statistics[0].RFCount != 2 {
		t.Fatalf("false source identities: %+v", s)
	}
}

func TestConcurrentReceiveSendAndSnapshot(t *testing.T) {
	tr, _ := testTracker()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				tr.Record(packet("STATION", 1, 44))
				tr.RecordMessageDownlink(1)
				tr.Snapshot()
			}
		}()
	}
	wg.Wait()
	if got := tr.Snapshot().Statistics[0]; got.MessageCount != 200 || got.RFCount != 1 {
		t.Fatalf("concurrent counts: %+v", got)
	}
}
