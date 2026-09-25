package messages

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

type capturedTextRF struct {
	channel uint32
	raw     []byte
	source  txgovernor.SubmitSource
}

func (f *capturedTextRF) Enabled(ch uint32) bool { return ch == 1 }
func (f *capturedTextRF) Submit(_ context.Context, ch uint32, raw []byte, src txgovernor.SubmitSource) error {
	f.channel, f.raw, f.source = ch, append([]byte(nil), raw...), src
	return nil
}

func TestSenderUsesDirectTNC2ForExtendedSource(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	textRF := &capturedTextRF{}
	rig.sender.cfg.TextRF = textRF
	row := newOutboundDM(t, rig, "F4MLV-GS", "F1ZDB-10", "direct text")
	result := rig.sender.Send(context.Background(), row)
	if result.Err != nil || result.Path != SendPathRF {
		t.Fatalf("send result = %+v", result)
	}
	if len(rig.sink.list()) != 0 {
		t.Fatal("textual message leaked to AX.25")
	}
	pkt, err := tnc2.Parse(textRF.raw)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Source.Text != "F4MLV-GS" || pkt.Destination.Text != "APGRWO" || !strings.Contains(string(pkt.Information), "direct text") {
		t.Fatalf("unexpected textual message: %s", textRF.raw)
	}
	if !textRF.source.SkipDedup || textRF.source.Kind != SubmitKindMessages {
		t.Fatalf("submit source = %+v", textRF.source)
	}
	before, _ := rig.store.GetByID(context.Background(), row.ID)
	if before.SentAt != nil {
		t.Fatal("marked sent before textual hook")
	}
	rig.sender.onTextTxComplete(textRF.channel, textRF.raw, textRF.source)
	after, _ := rig.store.GetByID(context.Background(), row.ID)
	if after.SentAt == nil {
		t.Fatal("textual hook did not mark message sent")
	}
}

func TestMessageAX25BoundaryRefusesNoncanonicalText(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "F4MLV-01", "F1ZDB-10", "identity test")
	if _, err := rig.sender.buildFrame(row); err == nil {
		t.Fatal("AX.25 message silently changed F4MLV-01 to F4MLV-1")
	}
	if _, err := preflightAckFrame("F4MLV-01", "F1ZDB-10", "123"); err == nil {
		t.Fatal("AX.25 ACK silently changed F4MLV-01 to F4MLV-1")
	}
	if _, err := parsePath("WIDE1-01"); err == nil {
		t.Fatal("AX.25 message path silently changed WIDE1-01 to WIDE1-1")
	}
	textRF := &capturedTextRF{}
	rig.sender.cfg.TextRF = textRF
	result := rig.sender.Send(context.Background(), row)
	if result.Err != nil {
		t.Fatalf("textual message rejected: %+v", result)
	}
	packet, err := tnc2.Parse(textRF.raw)
	if err != nil || packet.Source.Text != "F4MLV-01" {
		t.Fatalf("textual message identity changed: packet=%+v err=%v", packet, err)
	}
}

func TestPreflightAutoAckUsesDirectTNC2WithoutAX25(t *testing.T) {
	rf := &capturedTextRF{}
	sink := &fakeTxSink{}
	p, err := NewPreflight(PreflightConfig{
		OurCall: func() string { return "F4MLV-GS" },
		TxSink:  sink, TextRF: rf,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.SendAutoAck(context.Background(), &aprs.DecodedAPRSPacket{
		Direction: aprs.DirectionRF, TextualIngress: true, Channel: 1,
	}, "F1ZDB-10", "123")
	if len(sink.list()) != 0 {
		t.Fatal("textual ACK leaked to AX.25")
	}
	pkt, err := tnc2.Parse(rf.raw)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Source.Text != "F4MLV-GS" || string(pkt.Information) != ":F1ZDB-10 :ack123" {
		t.Fatalf("unexpected ACK: %s", rf.raw)
	}
}

func TestPreflightLeavesTextualAckToTransceiver(t *testing.T) {
	rf := &capturedTextRF{}
	sink := &fakeTxSink{}
	p, err := NewPreflight(PreflightConfig{
		OurCall: func() string { return "F4MLV-2" },
		TxSink:  sink, TextRF: rf, SuppressTextAutoAck: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.SendAutoAck(context.Background(), &aprs.DecodedAPRSPacket{
		Direction: aprs.DirectionRF, TextualIngress: true, Channel: 1,
	}, "F4MLV-7", "532")
	if rf.raw != nil || len(sink.list()) != 0 {
		t.Fatal("Graywolf sent a duplicate ACK for transceiver-owned TNC2 ingress")
	}
}
