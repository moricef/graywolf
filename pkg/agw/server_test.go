package agw

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

// fakeSink embeds the shared testtx.Recorder and adds a per-submit
// signal channel so tests can block until a frame has arrived
// without polling on Len().
type fakeSink struct {
	*testtx.Recorder
	ch chan struct{}
}

func newFakeSink() *fakeSink {
	s := &fakeSink{
		Recorder: testtx.NewRecorder(),
		ch:       make(chan struct{}, 16),
	}
	s.OnSubmit(func(testtx.Capture) { s.ch <- struct{}{} })
	return s
}

func TestAGWVersionAndSendUnproto(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		PortCallsigns: []string{"N0CALL-1"},
		Sink:          sink,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe(ctx) }()

	// Connect with retry.
	var conn net.Conn
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", srv.cfg.ListenAddr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not connect")
	}
	defer conn.Close()

	// Ask for version.
	if err := WriteFrame(conn, &Header{DataKind: KindVersion}, nil); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	h, data, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if h.DataKind != KindVersion || len(data) != 8 {
		t.Errorf("bad version response: %+v", h)
	}

	// Send an UNPROTO UI frame.
	if err := WriteFrame(conn, &Header{
		DataKind: KindSendUnproto,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "APRS",
	}, []byte("hello world")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sink.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("sink did not receive frame")
	}
	f := sink.Frames()[0]
	if f.Source.Call != "W1AW" || f.Dest.Call != "APRS" {
		t.Errorf("addrs: %+v / %+v", f.Source, f.Dest)
	}
	if string(f.Info) != "hello world" {
		t.Errorf("info: %q", f.Info)
	}
}

func TestAGWPortInfo(t *testing.T) {
	srv := NewServer(ServerConfig{
		PortCallsigns: []string{"N0CALL-1", "N0CALL-2"},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx) }()

	var conn net.Conn
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", srv.cfg.ListenAddr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("no connect")
	}
	defer conn.Close()

	_ = WriteFrame(conn, &Header{DataKind: KindPortInfo}, nil)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	h, data, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if h.DataKind != KindPortInfo {
		t.Errorf("kind: %c", h.DataKind)
	}
	if len(data) == 0 || data[0] != '2' {
		t.Errorf("port count wrong: %q", data)
	}
}

func TestInvertPortToChannel(t *testing.T) {
	tests := []struct {
		name          string
		portToChannel map[uint8]uint32
		channel       uint32
		want          uint8
	}{
		{name: "mapped channel", portToChannel: map[uint8]uint32{0: 1}, channel: 1, want: 0},
		{name: "unmapped channel defaults to port 0", portToChannel: map[uint8]uint32{0: 1}, channel: 2, want: 0},
		{name: "multiple ports", portToChannel: map[uint8]uint32{0: 1, 1: 2}, channel: 2, want: 1},
		{name: "duplicate mapping picks lowest port", portToChannel: map[uint8]uint32{2: 5, 0: 5}, channel: 5, want: 0},
		{name: "empty map defaults to port 0", portToChannel: nil, channel: 1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &Server{channelToPort: invertPortToChannel(tt.portToChannel)}
			if got := srv.portFor(tt.channel); got != tt.want {
				t.Errorf("portFor(%d) = %d, want %d", tt.channel, got, tt.want)
			}
		})
	}
}

// TestAGWBroadcastRawKISS proves the 'k' toggle end-to-end: a client that
// has enabled raw KISS reception gets a 'K' frame with the AGWPE port
// (translated from the graywolf channel, not the channel ID itself) folded
// into both the header and the leading TNC-indicator byte, while a client
// that hasn't gets nothing. Toggling twice returns the client to its
// original (off) state.
func TestAGWBroadcastRawKISS(t *testing.T) {
	srv := NewServer(ServerConfig{
		PortToChannel: map[uint8]uint32{0: 1, 1: 2},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx) }()

	dial := func() net.Conn {
		for i := 0; i < 50; i++ {
			c, err := net.Dial("tcp", srv.cfg.ListenAddr)
			if err == nil {
				return c
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("could not connect")
		return nil
	}

	enabled := dial()
	defer enabled.Close()
	quiet := dial()
	defer quiet.Close()

	// Only "enabled" toggles raw KISS reception on.
	if err := WriteFrame(enabled, &Header{DataKind: KindToggleRawKISS}, nil); err != nil {
		t.Fatal(err)
	}

	// Give the toggle time to land server-side before broadcasting;
	// dispatch runs on the server's per-connection goroutine.
	time.Sleep(50 * time.Millisecond)

	raw := []byte{0x01, 0x02, 0x03}
	srv.BroadcastRawKISS(2, raw) // channel 2 -> AGWPE port 1

	_ = enabled.SetReadDeadline(time.Now().Add(2 * time.Second))
	h, data, err := ReadFrame(enabled)
	if err != nil {
		t.Fatalf("enabled client: %v", err)
	}
	if h.DataKind != KindSendRaw {
		t.Errorf("kind = %c, want %c", h.DataKind, KindSendRaw)
	}
	if h.Port != 1 {
		t.Errorf("header port = %d, want 1", h.Port)
	}
	wantLeading := byte(1 << 4)
	if len(data) != len(raw)+1 || data[0] != wantLeading {
		t.Fatalf("payload = %v, want leading byte %#x followed by %v", data, wantLeading, raw)
	}
	if string(data[1:]) != string(raw) {
		t.Errorf("payload data = %v, want %v", data[1:], raw)
	}

	_ = quiet.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := ReadFrame(quiet); err == nil {
		t.Error("quiet client: expected no frame, got one")
	}

	// Toggle "enabled" back off; a second broadcast should reach no one.
	if err := WriteFrame(enabled, &Header{DataKind: KindToggleRawKISS}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	srv.BroadcastRawKISS(2, raw)
	_ = enabled.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := ReadFrame(enabled); err == nil {
		t.Error("toggled-off client: expected no frame, got one")
	}
}
