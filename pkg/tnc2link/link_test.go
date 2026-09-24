package tnc2link

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/tnc2"
)

func TestReadPacketsIgnoresSerialDiagnosticsAndPreservesExtendedMicE(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	link := NewLink(local)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	packets := make(chan *tnc2.TNC2Packet, 2)
	done := make(chan error, 1)
	go func() { done <- link.ReadPackets(ctx, func(p *tnc2.TNC2Packet) { packets <- p }) }()

	raw := append([]byte("F4MLV-GS>425WV3,WIDE2-1:"), 0x1d, 'w', '2', '6', 'l', 0x1c)
	lines := append([]byte("[123][I] Rx ---> ignored\r\nLOCAL -- RSSI:-74 SNR:+11.00 FO:+352\r\n"), raw...)
	lines = append(lines, []byte("\r\nWIDE2-1<--F4MLV-GS NA\r\n")...)
	if _, err := remote.Write(lines); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-packets:
		if !bytes.Equal(p.Raw, raw) || p.Source.Text != "F4MLV-GS" || !bytes.Equal(p.Information, raw[bytes.IndexByte(raw, ':')+1:]) {
			t.Fatalf("packet changed: %+v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("TNC2 packet was not received")
	}
	select {
	case p := <-packets:
		t.Fatalf("diagnostic line treated as packet: %+v", p)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ReadPackets cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadPackets did not stop")
	}
}

func TestSendPacketPreservesExtendedAddressAndEnforcesLineFraming(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	link := NewLimitedLink(local, 255)
	raw := []byte("F4MLV-GS>APLRG1,WIDE1-1::N7UV     :hello")
	done := make(chan error, 1)
	go func() { done <- link.SendPacket(context.Background(), raw) }()
	line, err := bufio.NewReader(remote).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if want := append(bytes.Clone(raw), '\r', '\n'); !bytes.Equal(line, want) {
		t.Fatalf("TX bytes = %q, want %q", line, want)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		[]byte("F4MLV-GS>APLRG1:hello\nOTHER>APRS:bad"),
		[]byte("F4MLV-GS>APLRG1:hello\rOTHER>APRS:bad"),
		[]byte(strings.Repeat("A", 256) + ">APRS:bad"),
		[]byte("not a packet"),
	} {
		if err := link.SendPacket(context.Background(), bad); err == nil {
			t.Fatalf("accepted invalid TX record %q", bad)
		}
	}
}

func TestClientSupportsTCPAndSerialOpeners(t *testing.T) {
	for _, transport := range []string{TransportTCP, TransportSerial} {
		t.Run(transport, func(t *testing.T) {
			local, remote := net.Pipe()
			defer remote.Close()
			opened := make(chan struct{}, 1)
			sent := make(chan []byte, 1)
			client := NewClient(ClientConfig{
				Transport: transport,
				OpenFunc: func(context.Context) (io.ReadWriteCloser, error) {
					opened <- struct{}{}
					return local, nil
				},
				OnPacket: func(*tnc2.TNC2Packet) {},
				OnSent:   func(raw []byte) { sent <- bytes.Clone(raw) },
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- client.Run(ctx) }()
			select {
			case <-opened:
			case <-time.After(time.Second):
				t.Fatal("transport was not opened")
			}
			deadline := time.Now().Add(time.Second)
			for !client.Connected() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !client.Connected() {
				t.Fatal("connected transport not exposed")
			}
			txDone := make(chan error, 1)
			go func() { txDone <- client.SendPacket(ctx, []byte("N0CALL>APRS:test")) }()
			if line, err := bufio.NewReader(remote).ReadString('\n'); err != nil || line != "N0CALL>APRS:test\r\n" {
				t.Fatalf("TX line %q: %v", line, err)
			}
			if err := <-txDone; err != nil {
				t.Fatal(err)
			}
			if err := client.EnqueuePacket([]byte("F4MLV-GS>APRS:queued")); err != nil {
				t.Fatal(err)
			}
			if line, err := bufio.NewReader(remote).ReadString('\n'); err != nil || line != "F4MLV-GS>APRS:queued\r\n" {
				t.Fatalf("queued TX line %q: %v", line, err)
			}
			select {
			case raw := <-sent:
				if string(raw) != "F4MLV-GS>APRS:queued" {
					t.Fatalf("OnSent bytes = %q", raw)
				}
			case <-time.After(time.Second):
				t.Fatal("successful queued TX was not reported")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("client did not stop")
			}
		})
	}
}
