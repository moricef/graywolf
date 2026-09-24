package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"github.com/chrissnell/graywolf/pkg/tnc2link"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func TestTNC2SettingsSwitchTCPConnectionWithoutRestart(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	accept := func(listener net.Listener) <-chan net.Conn {
		out := make(chan net.Conn, 1)
		go func() {
			conn, err := listener.Accept()
			if err == nil {
				out <- conn
			}
		}()
		return out
	}
	firstConn, secondConn := accept(first), accept(second)
	a := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	component := a.tnc2Component()
	if err := component.start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if err := component.stop(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	cfg := configstore.TNC2Config{TCPAddress: first.Addr().String(), SerialBaud: 115200, TXChannel: 1, MaxTXBytes: 255}
	a.applyTNC2Config(cfg)
	select {
	case conn := <-firstConn:
		defer conn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("first TNC2 connection not opened")
	}
	cfg.TCPAddress = second.Addr().String()
	a.applyTNC2Config(cfg)
	select {
	case conn := <-secondConn:
		defer conn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("updated TNC2 connection not opened")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, connected, _ := a.tnc2Settings()
		if got.TCPAddress == cfg.TCPAddress && connected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, connected, _ := a.tnc2Settings()
	t.Fatalf("live status=%+v connected=%v", got, connected)
}

func TestDirectTNC2MicEReceptionIsLosslessAndReceiveOnly(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()
	info := []byte{0x1d, 'd', ':', 0x1c, '(', '<', '>', '>', '/'}
	raw := append([]byte("F4JJE-16>35SP0P:"), info...)
	packet, err := tnc2.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	h.app.tnc2Produce("tnc2-serial", packet)
	entries := h.app.plog.Query(packetlog.Filter{Channel: -1})
	if len(entries) != 1 {
		t.Fatalf("packet log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.TNC2 == nil || !bytes.Equal(entry.TNC2.Raw, raw) || !bytes.Equal(entry.TNC2.Information, info) {
		t.Fatalf("direct TNC2 bytes changed: %+v", entry.TNC2)
	}
	if entry.Decoded == nil || entry.Decoded.Type != aprs.PacketMicE || entry.Decoded.Position == nil {
		t.Fatalf("Mic-E was not decoded: %+v", entry.Decoded)
	}
	stations := h.app.stationCache.QueryBBox(stationcache.BBox{
		SwLat: -90, SwLon: -180, NeLat: 90, NeLon: 180,
	}, time.Hour)
	if len(stations) != 1 || stations[0].Callsign != "F4JJE-16" {
		t.Fatalf("mapped stations = %+v", stations)
	}
	select {
	case packet := <-h.aprsOut:
		t.Fatalf("direct TNC2 reception reached APRS output: %+v", packet)
	default:
	}
	if h.digiEmits.Len() != 0 {
		t.Fatal("direct TNC2 reception reached digipeater output")
	}
}

func TestDirectTNC2MessageReachesComposerInbox(t *testing.T) {
	a, ctx, cancel := messagesWiringApp(t, "F4MLV-2")
	defer cancel()
	a.cfg.TNC2TXTransport = tnc2link.TransportTCP
	a.cfg.TNC2TXChannel = 1
	packet, err := tnc2.Parse([]byte("F4MLV-GS>APGRWO::F4MLV-2  :hello from LoRa{123"))
	if err != nil {
		t.Fatal(err)
	}
	a.tnc2Produce("tnc2-tcp", packet)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rows, _, err := a.msgStore.List(ctx, messages.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			if rows[0].FromCall != "F4MLV-GS" || rows[0].Text != "hello from LoRa" {
				t.Fatalf("inbox row = %+v", rows[0])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("direct TNC2 message did not reach inbox")
}

func TestDirectTNC2TXRequiresExplicitAuthorizationAndPreservesBytes(t *testing.T) {
	raw := append([]byte("F4MLV-GS>425WV3:"), 0x1d, 'w', '2', '6', 'l', 0x1c)
	a := &App{}
	if err := a.submitTNC2TX(context.Background(), raw); !errors.Is(err, errTNC2TXDisabled) {
		t.Fatalf("default TX policy = %v", err)
	}
	a.cfg.TNC2TXTransport = tnc2link.TransportTCP
	a.cfg.TNC2TXSource = "F4MLV-GS"
	a.cfg.TNC2TXChannel = 1
	a.cfg.TNC2MaxTXBytes = 255
	local, remote := net.Pipe()
	defer remote.Close()
	client := tnc2link.NewClient(tnc2link.ClientConfig{
		Transport:        tnc2link.TransportTCP,
		MaxTransmitBytes: 255,
		OpenFunc:         func(context.Context) (io.ReadWriteCloser, error) { return local, nil },
		OnPacket:         func(*tnc2.TNC2Packet) {},
	})
	a.tnc2ByTransport = map[string]*tnc2link.Client{tnc2link.TransportTCP: client}
	a.gov = txgovernor.New(txgovernor.Config{
		Sender:     func(*pb.TransmitFrame) error { t.Fatal("textual TX entered AX.25 output"); return nil },
		TextSender: a.sendTNC2Text,
		Channels:   map[uint32]txgovernor.ChannelTiming{1: {FullDup: true}},
	})
	if err := a.submitTNC2TX(context.Background(), raw); !errors.Is(err, tnc2link.ErrDisconnected) {
		t.Fatalf("disconnected TX policy = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for !client.Connected() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.Connected() {
		t.Fatal("TNC2 client failed to connect")
	}
	for _, bad := range [][]byte{
		[]byte("F4MLV-16>APRS:wrong source"),
		[]byte("F4MLV-GS>APRS:split\nOTHER>APRS:inject"),
		[]byte("F4MLV-GS>APRS:" + string(bytes.Repeat([]byte{'X'}, 256))),
	} {
		if err := a.submitTNC2TX(context.Background(), bad); err == nil {
			t.Fatalf("accepted unauthorized TX %q", bad)
		}
	}
	govDone := make(chan error, 1)
	go func() { govDone <- a.gov.Run(ctx) }()
	reader := bufio.NewReader(remote)
	for _, tc := range []struct {
		source string
		raw    []byte
	}{
		{"F4MLV-GS", raw}, // binary Mic-E
		{"F4MLV-GS", []byte("F4MLV-GS>APLRG1,WIDE1-1:!4338.25N/00031.99ELtext APRS")},
		{"F4MLV-16", []byte("F4MLV-16>NODE,RELAY*:non-APRS keyboard data")},
	} {
		a.cfg.TNC2TXSource = tc.source
		if err := a.submitTNC2TX(context.Background(), tc.raw); err != nil {
			t.Fatal(err)
		}
		_ = remote.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		if want := append(bytes.Clone(tc.raw), '\r', '\n'); !bytes.Equal(line, want) {
			t.Fatalf("TNC2 TX = %x, want %x", line, want)
		}
	}
	cancel()
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-govDone; err != nil {
		t.Fatal(err)
	}
}

func TestDirectTNC2Flags(t *testing.T) {
	cfg, err := parseFlagsTo([]string{
		"-tnc2-tcp", "192.0.2.1:8001",
		"-tnc2-serial", "/dev/ttyUSB0",
		"-tnc2-baud", "115200",
		"-tnc2-tx-transport", "tcp",
		"-tnc2-tx-source", "F4MLV-GS",
		"-tnc2-tx-channel", "3",
		"-tnc2-max-tx-bytes", "255",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TNC2TCP != "192.0.2.1:8001" || cfg.TNC2SerialDevice != "/dev/ttyUSB0" || cfg.TNC2SerialBaud != 115200 {
		t.Fatalf("TNC2 flags = %+v", cfg)
	}
	if cfg.TNC2TXTransport != "tcp" || cfg.TNC2TXSource != "F4MLV-GS" || cfg.TNC2TXChannel != 3 || cfg.TNC2MaxTXBytes != 255 {
		t.Fatalf("TNC2 TX flags = %+v", cfg)
	}
	if cfg.TNC2AutoAck {
		t.Fatal("TNC2 auto-ACK must be disabled by default")
	}
}

func TestDirectTNC2IsAValidDefaultTXChannel(t *testing.T) {
	a := &App{}
	a.cfg.TNC2TXTransport = tnc2link.TransportTCP
	a.cfg.TNC2TXChannel = 7
	if got := a.resolveTxChannel(context.Background(), 0); got != 7 {
		t.Fatalf("resolved TX channel = %d, want direct TNC2 channel 7", got)
	}
}
