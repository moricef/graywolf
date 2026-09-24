package beacon

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

type textBeaconSink struct {
	enabled bool
	raw     [][]byte
}

func (s *textBeaconSink) Enabled(uint32) bool { return s.enabled }
func (s *textBeaconSink) Submit(_ context.Context, _ uint32, raw []byte, source txgovernor.SubmitSource) error {
	if source.Kind != "beacon" {
		return errors.New("wrong submit source")
	}
	s.raw = append(s.raw, bytes.Clone(raw))
	return nil
}

func TestExtendedBeaconUsesTextRFAndTextAPRSIS(t *testing.T) {
	ax25Sink := testtx.NewRecorder()
	textSink := &textBeaconSink{enabled: true}
	isSink := &fakeISSink{}
	s, err := New(Options{Sink: ax25Sink, TextRF: textSink, ISSink: isSink})
	if err != nil {
		t.Fatal(err)
	}
	s.SetBeacons([]Config{{ID: 1, Type: TypePosition, Channel: 1, SourceText: "F4MLV-GS",
		DestText: "APGRWO", PathText: []string{"WIDE1-1"}, Lat: 42.9, Lon: 1.2,
		SymbolTable: '/', SymbolCode: '>', SendPath: SendPathBoth}})
	if err := s.SendNow(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(textSink.raw) != 1 || !bytes.HasPrefix(textSink.raw[0], []byte("F4MLV-GS>APGRWO,WIDE1-1:")) {
		t.Fatalf("text RF packets=%q", textSink.raw)
	}
	if ax25Sink.Len() != 0 {
		t.Fatal("text beacon leaked into AX.25 TX")
	}
	lines := isSink.Lines()
	if len(lines) != 1 || !bytes.HasPrefix([]byte(lines[0]), []byte("F4MLV-GS>APGRWO,TCPIP*:")) {
		t.Fatalf("APRS-IS lines=%q", lines)
	}
	textSink.enabled = false
	if err := s.SendNow(context.Background(), 1); err == nil {
		t.Fatal("extended beacon unexpectedly converted to AX.25 after text TX was disabled")
	}
	if ax25Sink.Len() != 0 {
		t.Fatal("extended beacon reached AX.25 after text TX disabled")
	}
}

func TestTextBeaconKeepsBinaryInformation(t *testing.T) {
	textSink := &textBeaconSink{enabled: true}
	s, err := New(Options{Sink: testtx.NewRecorder(), TextRF: textSink})
	if err != nil {
		t.Fatal(err)
	}
	info := string([]byte{0x1d, 'w', '2', '6', 'l', 0x1c, '[', '/'})
	s.SetBeacons([]Config{{ID: 2, Type: TypeCustom, Channel: 1, SourceText: "F4MLV-16", DestText: "425WV3", CustomInfo: info}})
	if err := s.SendNow(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	want := append([]byte("F4MLV-16>425WV3:"), []byte(info)...)
	if len(textSink.raw) != 1 || !bytes.Equal(textSink.raw[0], want) {
		t.Fatalf("binary text beacon=%q want=%q", textSink.raw, want)
	}
}

func TestExtendedMicEBeaconUsesTextRF(t *testing.T) {
	textSink := &textBeaconSink{enabled: true}
	ax25Sink := testtx.NewRecorder()
	s, err := New(Options{Sink: ax25Sink, TextRF: textSink})
	if err != nil {
		t.Fatal(err)
	}
	s.SetBeacons([]Config{{ID: 3, Type: TypePosition, Channel: 1,
		SourceText: "F4MLV-GS", DestText: "APGRWO", Format: "mic_e",
		Lat: 42.9, Lon: 1.2, SymbolTable: '/', SymbolCode: '>', Messaging: true}})
	if err := s.SendNow(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if len(textSink.raw) != 1 || ax25Sink.Len() != 0 {
		t.Fatalf("text=%q AX.25 count=%d", textSink.raw, ax25Sink.Len())
	}
	packet, err := tnc2.Parse(textSink.raw[0])
	if err != nil {
		t.Fatal(err)
	}
	if packet.Source.Text != "F4MLV-GS" || packet.Destination.Text != MicEDestination(42.9, 1.2, 0) {
		t.Fatalf("Mic-E addresses: source=%q destination=%q", packet.Source.Text, packet.Destination.Text)
	}
	decoded, err := aprs.ParseTNC2Packet(packet)
	if err != nil || decoded.Type != aprs.PacketMicE || decoded.MicE == nil {
		t.Fatalf("Mic-E decode: packet=%+v err=%v", decoded, err)
	}
	if !bytes.Equal(packet.Information, []byte(MicEPositionInfo(42.9, 1.2, 0, 0, 0, '/', '>', true, 0, ""))) {
		t.Fatalf("Mic-E information changed: %x", packet.Information)
	}
}
