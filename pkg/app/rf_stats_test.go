package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func TestIGateMessageCountAtSuccessfulRFOutput(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "failed"}[failure], func(t *testing.T) {
			a := &App{rfDX: rfdx.New(nil)}
			sent := make(chan struct{}, 2)
			g := txgovernor.New(txgovernor.Config{Channels: map[uint32]txgovernor.ChannelTiming{1: {FullDup: true}}, Sender: func(*pb.TransmitFrame) error {
				if failure {
					sent <- struct{}{}
					return errors.New("disconnected sender")
				}
				return nil
			}})
			g.AddTxHook(func(ch uint32, f *ax25.Frame, src txgovernor.SubmitSource) {
				p, _ := aprs.Parse(f)
				a.recordIGateMessageTX(ch, p, src)
				sent <- struct{}{}
			})
			src, _ := ax25.ParseAddress("F4MLV-2")
			dst, _ := ax25.ParseAddress("APGRWO")
			frame := &ax25.Frame{Source: src, Dest: dst, Control: 0x03, PID: 0xf0, Info: []byte("}ORIGIN>APRS,TCPIP,F4MLV-2*::TARGET   :hello{01")}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := g.Submit(ctx, 1, frame, txgovernor.SubmitSource{Kind: "igate", Detail: "is2rf"}); err != nil {
				t.Fatal(err)
			}
			if len(a.rfDX.Snapshot().Statistics) != 0 {
				t.Fatal("queued frame counted before downstream acceptance")
			}
			done := make(chan struct{})
			go func() { defer close(done); _ = g.Run(ctx) }()
			defer func() { cancel(); <-done }()
			select {
			case <-sent:
			case <-time.After(2 * time.Second):
				t.Fatal("sender not called")
			}
			stats := a.rfDX.Snapshot().Statistics
			if failure {
				if len(stats) != 0 {
					t.Fatalf("failed send counted: %+v", stats)
				}
			} else if len(stats) != 1 || stats[0].MessageCount != 1 {
				t.Fatalf("successful send not counted: %+v", stats)
			}
		})
	}
}

func TestIGateMessageCountExcludesOtherTXKinds(t *testing.T) {
	a := &App{rfDX: rfdx.New(nil)}
	message := &aprs.DecodedAPRSPacket{ThirdParty: &aprs.DecodedAPRSPacket{Message: &aprs.Message{}}}
	for _, src := range []txgovernor.SubmitSource{{Kind: "messages"}, {Kind: "beacon"}, {Kind: "digipeater"}, {Kind: "igate", Detail: "other"}} {
		a.recordIGateMessageTX(1, message, src)
	}
	a.recordIGateMessageTX(1, &aprs.DecodedAPRSPacket{}, txgovernor.SubmitSource{Kind: "igate", Detail: "is2rf"})
	if len(a.rfDX.Snapshot().Statistics) != 0 {
		t.Fatal("non-downlinked message counted")
	}
}
