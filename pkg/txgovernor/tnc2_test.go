package txgovernor

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestSubmitTNC2KeepsExtendedIdentityAndBytes(t *testing.T) {
	var mu sync.Mutex
	var sent [][]byte
	ax25Calls := 0
	g := New(Config{
		Sender: func(*pb.TransmitFrame) error { ax25Calls++; return nil },
		TextSender: func(_ uint32, raw []byte, _ SubmitSource) error {
			mu.Lock()
			sent = append(sent, bytes.Clone(raw))
			mu.Unlock()
			return nil
		},
		Logger:     silentLogger(),
		RandSource: rand.New(rand.NewSource(1)),
		Channels:   map[uint32]ChannelTiming{1: {FullDup: true}},
	})
	raw := append([]byte("F4MLV-GS>35SP0P,WIDE2-1:"), 0x1d, 'd', ':', 0x1c, '(', '<', '>')
	want := bytes.Clone(raw)
	if err := g.SubmitTNC2(context.Background(), 1, raw, SubmitSource{Kind: "tnc2", Priority: PriorityClient}); err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X' // caller buffer reuse must not change the queued record
	g.processOne(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || !bytes.Equal(sent[0], want) {
		t.Fatalf("textual TX = %x, want %x", sent, want)
	}
	if ax25Calls != 0 {
		t.Fatalf("textual TX entered AX.25 sender %d times", ax25Calls)
	}
}

func TestSubmitTNC2RejectsInvalidAndMissingSender(t *testing.T) {
	g := New(Config{Sender: func(*pb.TransmitFrame) error { return nil }, Logger: silentLogger()})
	if err := g.SubmitTNC2(context.Background(), 1, []byte("F4MLV-GS>APRS:test"), SubmitSource{}); !errors.Is(err, ErrNoTextSender) {
		t.Fatalf("missing sender error = %v", err)
	}
	g.cfg.TextSender = func(uint32, []byte, SubmitSource) error { return nil }
	for _, raw := range [][]byte{
		[]byte("F4MLV-GS>APRS:test\nINJECT>APRS:bad"),
		[]byte("not a packet"),
	} {
		if err := g.SubmitTNC2(context.Background(), 1, raw, SubmitSource{}); err == nil {
			t.Fatalf("accepted invalid TNC2 record %q", raw)
		}
	}
}

func TestTextTxHookFiresOnlyAfterSenderAccepts(t *testing.T) {
	accepted := false
	g := New(Config{
		Sender: func(*pb.TransmitFrame) error { return nil },
		TextSender: func(uint32, []byte, SubmitSource) error {
			if !accepted {
				return errors.New("peer queue full")
			}
			return nil
		},
		Logger:   silentLogger(),
		Channels: map[uint32]ChannelTiming{1: {FullDup: true}},
	})
	hooks := 0
	_, unregister := g.AddTextTxHook(func(_ uint32, raw []byte, src SubmitSource) {
		if string(raw) != "F4MLV-GS>APRS:hello" || src.Kind != "messages" {
			t.Fatal("wrong hook packet")
		}
		hooks++
	})
	for i := 0; i < 2; i++ {
		if err := g.SubmitTNC2(context.Background(), 1, []byte("F4MLV-GS>APRS:hello"), SubmitSource{Kind: "messages", SkipDedup: true}); err != nil {
			t.Fatal(err)
		}
		g.processOne(context.Background())
		accepted = true
	}
	if hooks != 1 {
		t.Fatalf("hook calls = %d, want 1", hooks)
	}
	unregister()
}
