package kiss

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Independent wire fixtures exercise the documented 36-byte header rather
// than using the same AGW codec for both ends of the compatibility test.
func agwFixture(kind byte, port byte, data []byte) []byte {
	b := make([]byte, 36+len(data))
	b[0] = port
	b[4] = kind
	binary.LittleEndian.PutUint32(b[28:32], uint32(len(data)))
	copy(b[36:], data)
	return b
}
func readAGWFixture(t *testing.T, conn net.Conn) (byte, byte, []byte) {
	t.Helper()
	b := make([]byte, 36)
	if _, err := io.ReadFull(conn, b); err != nil {
		t.Fatal(err)
	}
	n := binary.LittleEndian.Uint32(b[28:32])
	if n > 65536 {
		t.Fatal("oversized payload")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(conn, data); err != nil {
		t.Fatal(err)
	}
	return b[4], b[0], data
}

func TestAGWClientAPRSAndReconnect(t *testing.T) {
	rx := make(chan *pb.ReceivedFrame, 4)
	cli := newClient(ClientConfig{TCPProtocol: "agwpe", AgwPort: 1, InterfaceID: 42,
		Mode: ModeTnc, AllowTxFromGovernor: true, ChannelMap: map[uint8]uint32{0: 7}, Logger: silentLogger(),
		RxIngress: func(rf *pb.ReceivedFrame, src ingress.Source) {
			if src != ingress.KissTnc(42) {
				t.Errorf("lost interface origin: %+v", src)
			}
			rx <- rf
		},
	})
	defer cli.closeTxQueue()
	// Two sequential sessions verify fresh discovery and exactly one raw toggle
	// after reconnect, with receive/transmit isolated to the selected radio.
	for session := 0; session < 2; session++ {
		server, client := net.Pipe()
		_ = server.SetDeadline(time.Now().Add(3 * time.Second))
		ctx, cancel := context.WithCancel(context.Background())
		cli.cfg.DialFunc = func(context.Context, string, string) (net.Conn, error) { return client, nil }
		dialDone := make(chan error, 1)
		go func() { _, err := cli.dial(ctx); dialDone <- err }()
		kind, _, data := readAGWFixture(t, server)
		if kind != 'G' || len(data) != 0 {
			t.Fatal("expected port discovery")
		}
		// A version reply may be interleaved and must not be mistaken for data.
		_, _ = server.Write(agwFixture('R', 0, make([]byte, 8)))
		_, _ = server.Write(agwFixture('G', 0, []byte("2;Port1 VHF;Port2 UHF;\x00")))
		kind, _, _ = readAGWFixture(t, server)
		if kind != 'k' {
			t.Fatal("raw reception not enabled")
		}
		if err := <-dialDone; err != nil {
			t.Fatal(err)
		}
		cli.mu.Lock()
		cli.conn = client
		cli.mu.Unlock()
		readDone := make(chan error, 1)
		go func() { readDone <- cli.frameLoop(ctx, client) }()
		raw := uiFrame(t, "F6DEV-7", "APRS", "!4903.50N/07201.75W>binary\xc0\xdb\x00")
		// Receive just one frame despite another port and duplicate text monitor.
		_, _ = server.Write(agwFixture('K', 0, append([]byte{0}, raw...)))
		_, _ = server.Write(agwFixture('U', 1, []byte("monitor text")))
		_, _ = server.Write(agwFixture('K', 1, append([]byte{0x10}, raw...)))
		select {
		case rf := <-rx:
			if rf.Channel != 7 {
				t.Fatalf("wrong channel %d", rf.Channel)
			}
		case <-time.After(time.Second):
			t.Fatal("missing AGW RX")
		}
		select {
		case <-rx:
			t.Fatal("other port/monitor leaked into RX")
		default:
		}
		writeDone := make(chan struct{})
		go func() { cli.writeFrame(7, raw); close(writeDone) }()
		kind, port, data := readAGWFixture(t, server)
		if kind != 'K' || port != 1 || !bytes.Equal(data, append([]byte{0x10}, raw...)) {
			t.Fatalf("bad raw TX: %c/%d/%x", kind, port, data)
		}
		<-writeDone
		cancel()
		_ = server.Close()
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Fatal("reader did not stop")
		}
		cli.mu.Lock()
		cli.conn = nil
		cli.mu.Unlock()
	}
}

func TestAGWClientDiscoveryFailures(t *testing.T) {
	for _, ports := range []string{"0;", "1;Port1 VHF;", "bad;", "2;Port1 only;"} {
		t.Run(ports, func(t *testing.T) {
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			_ = server.SetDeadline(time.Now().Add(time.Second))
			cli := newClient(ClientConfig{TCPProtocol: "agwpe", AgwPort: 1, Logger: silentLogger(), DialFunc: func(context.Context, string, string) (net.Conn, error) { return client, nil }})
			done := make(chan error, 1)
			go func() { _, err := cli.dial(context.Background()); done <- err }()
			readAGWFixture(t, server)
			_, _ = server.Write(agwFixture('G', 0, []byte(ports)))
			if err := <-done; err == nil {
				t.Fatal("invalid/unavailable port accepted")
			}
		})
	}
}

func TestAGWClientCancelDuringInitialization(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	cli := newClient(ClientConfig{TCPProtocol: "agwpe", Logger: silentLogger(), DialFunc: func(context.Context, string, string) (net.Conn, error) { return client, nil }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := cli.dial(ctx); done <- err }()
	_ = server.SetDeadline(time.Now().Add(time.Second))
	readAGWFixture(t, server)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled initialization succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("initialization hung")
	}
}

func TestAGWClientSupervisorReconnects(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	address := ln.Addr().(*net.TCPAddr)
	cli := newClient(ClientConfig{TCPProtocol: "agwpe", RemoteHost: "127.0.0.1", RemotePort: uint16(address.Port), Mode: ModeTnc,
		ReconnectInitMs: 10, ReconnectMaxMs: 20, Logger: silentLogger()})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		select {
		case <-cli.done:
		case <-time.After(time.Second):
			t.Error("supervisor did not stop")
		}
	}()
	go cli.run(ctx)
	for session := uint64(1); session <= 2; session++ {
		_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(2 * time.Second))
		conn, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		kind, _, _ := readAGWFixture(t, conn)
		if kind != 'G' {
			t.Fatal("no rediscovery")
		}
		_, _ = conn.Write(agwFixture('G', 0, []byte("1;Port1 test;")))
		kind, _, _ = readAGWFixture(t, conn)
		if kind != 'k' {
			t.Fatal("raw toggle missing")
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && (cli.Status().ReconnectCount < session || cli.Status().State != StateConnected) {
			time.Sleep(time.Millisecond)
		}
		if st := cli.Status(); st.State != StateConnected || st.ReconnectCount != session {
			t.Fatalf("not ready after reconnect: %+v", st)
		}
		_ = conn.Close()
	}
}
