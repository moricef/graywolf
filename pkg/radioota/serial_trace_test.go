package radioota

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSerialTraceRedactsFramesAndSecrets(t *testing.T) {
	secret := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	tag := "OLkaJxyKIXBM9SrF"
	frame := bytes.Repeat([]byte{0xab}, 179)
	copy(frame, []byte("\x3c\xff\x7e\x02\x10"))
	binary.BigEndian.PutUint32(frame[13:17], 256)
	for _, prefix := range []string{"TX", "TXRAW", "RX"} {
		fields := serialTraceFields(prefix + " " + hex.EncodeToString(frame))
		if fields["frame_bytes"] != 179 || fields["op"] != 16 || fields["offset"] != uint32(256) {
			t.Fatal(fields)
		}
		raw, _ := json.Marshal(fields)
		if strings.Contains(string(raw), "abab") {
			t.Fatal("frame data leaked", string(raw))
		}
	}
	for _, line := range []string{"OTAPROVISION F4MLV-2 " + secret, "!RC1:A:1Z:TX=OFF:" + tag, "RX broken " + secret} {
		raw, _ := json.Marshal(serialTraceFields(line))
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), tag) {
			t.Fatal("secret or signature leaked", string(raw))
		}
	}
	for _, line := range []string{"TXDONE 0", "TXDONE -1", "ERROR frame", "[LORA] OTA frame transmit error -5", "RXMETA rssi=-80.00 snr=7.50 fo=200"} {
		if serialTraceFields(line)["line"] != line {
			t.Fatal("diagnostic changed", line)
		}
	}
	if len(serialTraceText(strings.Repeat("x ", 2000))) > 1040 {
		t.Fatal("unbounded diagnostic")
	}
}

type traceRead struct {
	data string
	err  error
}

type tracePort struct {
	*fakeOTA
	reads    []traceRead
	writeErr error
}

func (p *tracePort) Write(buf []byte) (int, error) {
	if p.writeErr != nil {
		return 0, p.writeErr
	}
	return len(buf), nil
}

func (p *tracePort) Read(buf []byte) (int, error) {
	if len(p.reads) == 0 {
		p.clock.Sleep(100 * time.Millisecond)
		return 0, nil
	}
	r := p.reads[0]
	p.reads = p.reads[1:]
	return copy(buf, r.data), r.err
}

func TestSerialTracePartialAndFailedReads(t *testing.T) {
	p := &tracePort{fakeOTA: newFakeOTA(0x51524c50), reads: []traceRead{{"TXDO", nil}, {"", nil}, {"NE 0\r\nERROR frame\n", nil}, {"", io.ErrUnexpectedEOF}}}
	var events []string
	b := &bridge{port: p, clock: p.clock, trace: func(name string, fields map[string]any) {
		encoded, _ := json.Marshal(fields)
		events = append(events, name+" "+string(encoded))
	}}
	if line, err := b.line(); line != "" || err != nil {
		t.Fatal(line, err)
	}
	if line, err := b.line(); line != "TXDONE 0" || err != nil {
		t.Fatal(line, err)
	}
	if line, err := b.line(); line != "ERROR frame" || err != nil {
		t.Fatal(line, err)
	}
	if _, err := b.line(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	text := strings.Join(events, "\n")
	for _, want := range []string{"usb_read", "usb_rx_partial", "TXDONE 0", "ERROR frame", "usb_read_error"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
}

func TestNativeSerialTraceIsInTransferLog(t *testing.T) {
	p := newFakeOTA(0x51524c50)
	p.image = bytes.Repeat([]byte{0xa7}, 512)
	args := nativeArgs(t, p)
	var out bytes.Buffer
	if err := runNative(args, &out, func(string) (otaPort, error) { return p, nil }, p.clock); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(p.pauseFile), "transfer.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"usb_tx", "OTABRIDGE 921600", "usb_rx", "TXDONE 0", "post_install_verified"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatal("missing trace", want)
		}
	}
	for _, frame := range p.txFrames {
		if bytes.Contains(data, []byte(hex.EncodeToString(frame))) {
			t.Fatal("frame authentication tag leaked")
		}
	}
}

func TestFirmwareBlockTimeoutIdentifiesUSBController(t *testing.T) {
	p := &tracePort{fakeOTA: newFakeOTA(0x51524c50), reads: []traceRead{{"TXDO", nil}}}
	var events []string
	e := &nativeEngine{bridge: &bridge{port: p, clock: p.clock}, clock: p.clock, key: p.key, source: p.source, target: p.target, transfer: settings().Transfer, event: func(name string, _ map[string]any) { events = append(events, name) }}
	err := e.sendBlock(256, bytes.Repeat([]byte{1}, 128))
	if !errors.Is(err, errRadioTimeout) || !strings.Contains(err.Error(), "USB controller TXDONE") {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(events, ","), "bridge_tx_timeout") {
		t.Fatal(events)
	}
}

func TestSerialTraceWriteError(t *testing.T) {
	p := &tracePort{fakeOTA: newFakeOTA(0x51524c50), writeErr: io.ErrClosedPipe}
	var event string
	b := &bridge{port: p, clock: p.clock, trace: func(name string, _ map[string]any) { event = name }}
	if err := b.write("PROFILE 0\n"); !errors.Is(err, io.ErrClosedPipe) || event != "usb_write_error" {
		t.Fatal(err, event)
	}
}
