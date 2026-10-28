package beacon

import (
	"context"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
	"github.com/chrissnell/graywolf/pkg/rfdx"
)

func TestStationReportRoutesAndEmptyInfo(t *testing.T) {
	for _, sendPath := range []string{SendPathRF, SendPathBoth, SendPathISOnly} {
		t.Run(sendPath, func(t *testing.T) {
			rf := testtx.NewRecorder()
			is := &fakeISSink{}
			text := &textBeaconSink{enabled: true}
			s, err := New(Options{Sink: rf, ISSink: is, TextRF: text})
			if err != nil {
				t.Fatal(err)
			}
			src, _ := ax25.ParseAddress("F4MLV-2")
			dst, _ := ax25.ParseAddress("APGRWO")
			config := Config{Type: TypeCustom, CustomInfo: "<IGATE,DX=F5ZQB-10(139km@136deg)", Channel: 2, Source: src, Dest: dst, SourceText: "F4MLV-2", DestText: "APGRWO", SendPath: sendPath}
			if err := s.SendReport(context.Background(), config); err != nil {
				t.Fatal(err)
			}
			if rf.Len() != 0 {
				t.Fatal("text report leaked into AX.25 output")
			}
			wantRF, wantIS := 1, 1
			if sendPath == SendPathISOnly {
				wantRF = 0
			}
			if sendPath == SendPathRF {
				wantIS = 0
			}
			if len(text.raw) != wantRF || len(is.Lines()) != wantIS {
				t.Fatalf("RF=%d IS=%d", len(text.raw), len(is.Lines()))
			}
			for _, line := range is.Lines() {
				if !strings.Contains(line, ":<IGATE,DX=F5ZQB-10(139km@136deg)") {
					t.Fatalf("bad line %s", line)
				}
			}
			config.CustomInfo = ""
			if err := s.SendReport(context.Background(), config); err == nil {
				t.Fatal("empty information field should be rejected")
			}
			if len(text.raw) != wantRF || len(is.Lines()) != wantIS {
				t.Fatal("empty window transmitted")
			}
		})
	}
}

func TestStatisticsBeaconWithoutDXUsesKissAndIS(t *testing.T) {
	tracker := rfdx.New(nil)
	tracker.Record(&aprs.DecodedAPRSPacket{Source: "DIRECT", Channel: 1, Direction: aprs.DirectionRF})
	tracker.Record(&aprs.DecodedAPRSPacket{Source: "VIA", Channel: 1, Direction: aprs.DirectionRF, Path: []string{"DIGI*"}})
	tracker.RecordMessageDownlink(1)
	rf := testtx.NewRecorder()
	is := &fakeISSink{}
	s, err := New(Options{Sink: rf, ISSink: is})
	if err != nil {
		t.Fatal(err)
	}
	src, _ := ax25.ParseAddress("F4MLV-2")
	dst, _ := ax25.ParseAddress("APGRWO")
	info, _ := tracker.Report(1, "capabilities", "", true)
	if err := s.SendReport(context.Background(), Config{Type: TypeCustom, CustomInfo: info, Channel: 1, Source: src, Dest: dst, SendPath: SendPathBoth}); err != nil {
		t.Fatal(err)
	}
	const want = "<IGATE,MSG_CNT=1,LOC_CNT=1,DIR_CNT=1,RF_CNT=2"
	if rf.Len() != 1 || string(rf.Frames()[0].Info) != want {
		t.Fatalf("KISS report: %+v", rf.Frames())
	}
	if lines := is.Lines(); len(lines) != 1 || !strings.HasSuffix(lines[0], ":"+want) {
		t.Fatalf("IS report: %q", lines)
	}
}
