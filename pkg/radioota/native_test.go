package radioota

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

type fakeClock struct {
	at      time.Time
	onSleep func(time.Duration)
}

func (c *fakeClock) Now() time.Time { return c.at }
func (c *fakeClock) Sleep(d time.Duration) {
	c.at = c.at.Add(d)
	if c.onSleep != nil {
		c.onSleep(d)
	}
}

// This simulated device parses the firmware's wire layout independently of the
// client encoder/decoder. It enforces authentication, addresses, nonce, model,
// page commit and boot confirmation. No serial hardware is touched.
type fakeOTA struct {
	hostBaud, deviceBaud       int
	txFrames                   [][]byte
	firstNonce                 *[8]byte
	clock                      *fakeClock
	rx                         bytes.Buffer
	key, routeKey              []byte
	source, target, relay      string
	board                      uint32
	nonce                      [8]byte
	lastCounter                uint32
	image, received            []byte
	sha                        [32]byte
	committed                  uint32
	mask                       uint32
	currentBase                uint32
	running, boot              uint32
	pending, closed, bridgeOff bool
	version                    string
	manifestCount              int
	dataOffsets                []uint32
	ops                        []byte
	baud                       []int
	queryDrops                 int
	fault                      string
	once                       bool
	failOpenStep               string
	rollback                   bool
	pauseFile                  string
}

func newFakeOTA(board uint32) *fakeOTA {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return &fakeOTA{clock: &fakeClock{at: time.Unix(1800000000, 0)}, key: key, routeKey: bytes.Repeat([]byte{0x42}, 32), source: "F4MLV-2", target: "F4MLV-15", board: board, deviceBaud: 115200, hostBaud: 115200, running: 0x10000, boot: 0x10000, version: "old"}
}
func (p *fakeOTA) SetMode(m *serial.Mode) error {
	p.baud = append(p.baud, m.BaudRate)
	p.hostBaud = m.BaudRate
	if p.failOpenStep == "mode" {
		return errors.New("mode failed")
	}
	return nil
}
func (p *fakeOTA) SetReadTimeout(time.Duration) error {
	if p.failOpenStep == "timeout" {
		return errors.New("timeout failed")
	}
	return nil
}
func (p *fakeOTA) ResetInputBuffer() error {
	if p.failOpenStep == "reset" {
		return errors.New("reset failed")
	}
	p.rx.Reset()
	return nil
}
func (p *fakeOTA) Drain() error { return nil }
func (p *fakeOTA) Close() error { p.closed = true; return nil }
func (p *fakeOTA) Read(b []byte) (int, error) {
	if p.rx.Len() > 0 {
		return p.rx.Read(b)
	}
	p.clock.Sleep(100 * time.Millisecond)
	return 0, nil
}
func independentTag(key []byte, domain string, raw []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(domain))
	h.Write(raw)
	return h.Sum(nil)[:16]
}
func (p *fakeOTA) reply(op, status byte, offset uint32, body []byte) {
	payload := make([]byte, 6)
	payload[0] = op
	payload[1] = status
	binary.BigEndian.PutUint32(payload[2:], p.committed)
	switch op {
	case 15, 17:
		mask := p.mask
		if body != nil {
			mask = binary.BigEndian.Uint32(body)
		}
		payload = binary.BigEndian.AppendUint32(payload, mask)
	case 14:
		payload = append(payload, 0, 0, 0)
	default:
		pending := byte(0)
		if p.pending {
			pending = 1
		}
		version := p.version
		if p.fault == "wrong-final-version" && !p.pending && p.running != 0x10000 {
			version = "wrong"
		}
		payload = append(payload, pending)
		payload = binary.BigEndian.AppendUint32(payload, p.running)
		payload = binary.BigEndian.AppendUint32(payload, p.boot)
		payload = append(payload, byte(len(version)))
		payload = append(payload, []byte(version)...)
		payload = append(payload, p.sha[:]...)
	}
	raw := []byte{0x3c, 0xff, 0x7e, 2, 128}
	raw = append(raw, p.nonce[:]...)
	raw = binary.BigEndian.AppendUint32(raw, offset)
	raw = binary.BigEndian.AppendUint16(raw, uint16(len(payload)))
	raw = append(raw, identity(p.target)...)
	raw = append(raw, identity(p.source)...)
	raw = append(raw, payload...)
	raw = append(raw, independentTag(p.key, "LORA-APRS-OTA2", raw)...)
	if p.relay != "" {
		hop := []byte{0x3c, 0xff, 0x7d, 1, 3}
		hop = append(hop, identity(p.relay)...)
		hop = append(hop, raw...)
		raw = append(hop, independentTag(p.routeKey, "LORA-APRS-OTA-HOP1", hop)...)
	}
	fmt.Fprintf(&p.rx, "RX %x\n", raw)
}
func (p *fakeOTA) Write(b []byte) (int, error) {
	line := strings.TrimSpace(string(b))
	switch {
	case strings.HasPrefix(line, "OTABRIDGE "):
		if p.hostBaud != p.deviceBaud {
			return len(b), nil
		}
		if p.failOpenStep != "handshake" {
			p.rx.WriteString("OTABRIDGE OK\n")
			baud, err := strconv.Atoi(strings.TrimPrefix(line, "OTABRIDGE "))
			if err != nil {
				return 0, err
			}
			p.deviceBaud = baud
		}
	case strings.HasPrefix(line, "PROFILEDEF "):
		fields := strings.Fields(line)
		fmt.Fprintf(&p.rx, "PROFILEDEF %s OK\n", fields[1])
	case strings.HasPrefix(line, "PROFILE "):
		p.rx.WriteString(line + " OK\n")
	case line == "BRIDGE OFF":
		p.bridgeOff = true
		if p.fault != "restore" {
			p.rx.WriteString("BRIDGE OFF OK\n")
		}
	case strings.HasPrefix(line, "TX "):
		p.rx.WriteString("TXDONE 0\n")
		raw, err := hex.DecodeString(strings.TrimPrefix(line, "TX "))
		p.txFrames = append(p.txFrames, append([]byte{}, raw...))
		if err != nil {
			return 0, err
		}
		if p.relay != "" {
			if len(raw) < 80 || raw[4] != 0 || !hmac.Equal(raw[len(raw)-16:], independentTag(p.routeKey, "LORA-APRS-OTA-HOP1", raw[:len(raw)-16])) {
				return len(b), nil
			}
			raw = raw[13 : len(raw)-16]
		}
		if len(raw) < 51 || !bytes.Equal(raw[:4], []byte{0x3c, 0xff, 0x7e, 2}) || !hmac.Equal(raw[len(raw)-16:], independentTag(p.key, "LORA-APRS-OTA2", raw[:len(raw)-16])) {
			return len(b), nil
		}
		if !bytes.Equal(raw[19:27], identity(p.source)) || !bytes.Equal(raw[27:35], identity(p.target)) {
			return 0, errors.New("wrong identities")
		}
		op := raw[4]
		offset := binary.BigEndian.Uint32(raw[13:17])
		payload := raw[35 : len(raw)-16]
		p.ops = append(p.ops, op)
		if int(binary.BigEndian.Uint16(raw[17:19])) != len(payload) {
			return 0, errors.New("wrong body size")
		}
		if op == 6 {
			if offset <= p.lastCounter {
				return len(b), nil
			}
			first := p.lastCounter == 0
			p.lastCounter = offset
			p.nonce[0]++
			p.nonce[7] = 99
			if first && p.firstNonce != nil {
				p.nonce = *p.firstNonce
			}
		} else if !bytes.Equal(raw[5:13], p.nonce[:]) {
			return len(b), nil
		}
		switch op {
		case 5:
			p.committed = 0
			p.mask = 0
		case 15:
			p.manifestCount++
			if len(payload) != 40 || binary.BigEndian.Uint32(payload[36:40]) != p.board || p.fault == "format" {
				p.reply(op, 1, offset, nil)
				return len(b), nil
			}
			copy(p.sha[:], payload[4:36])
			size := binary.BigEndian.Uint32(payload[:4])
			if len(p.received) == 0 {
				p.received = make([]byte, size)
			}
		case 16:
			if len(payload) > 128 {
				return 0, errors.New("oversized block")
			}
			p.dataOffsets = append(p.dataOffsets, offset)
			if p.fault == "pause" && !p.once {
				p.once = true
				os.WriteFile(p.pauseFile, []byte("pause"), 0600)
			}
			if p.fault == "hole" && !p.once && offset == 128 {
				p.once = true
				return len(b), nil
			}
			base := offset / 4096 * 4096
			if base != p.currentBase {
				p.mask = 0
				p.currentBase = base
			}
			copy(p.received[offset:], payload)
			p.mask |= 1 << ((offset - base) / 128)
			return len(b), nil
		case 17:
			if p.fault == "query-timeout" && p.queryDrops < 2 {
				p.queryDrops++
				p.mask = 0
				return len(b), nil
			}
			mask := binary.BigEndian.Uint32(payload)
			if p.mask == mask {
				p.committed = min(offset+4096, uint32(len(p.received)))
			}
			if p.fault == "bitmap" {
				bad := uint32(0x80000000)
				p.reply(op, 0, offset, binary.BigEndian.AppendUint32(nil, bad))
				return len(b), nil
			}
		case 18:
			if sha256.Sum256(p.received) != p.sha || p.committed != uint32(len(p.received)) {
				return 0, errors.New("image verification failed")
			}
		case 19, 10:
			p.running = 0x1f0000
			p.boot = p.running
			p.pending = true
			p.version = "new"
			if p.fault == "activate-timeout" {
				return len(b), nil
			}
		case 7:
			if !bytes.Equal(payload, p.sha[:]) {
				return 0, errors.New("wrong confirmation hash")
			}
			p.pending = false
		}
		p.reply(op, 0, offset, nil)
	default:
		return 0, fmt.Errorf("unexpected console command %q", line)
	}
	return len(b), nil
}
func nativeArgs(t *testing.T, p *fakeOTA) []string {
	t.Helper()
	root := t.TempDir()
	for name, data := range map[string][]byte{"target.key": []byte(base64.RawURLEncoding.EncodeToString(p.key)), "route.key": []byte(base64.RawURLEncoding.EncodeToString(p.routeKey)), "firmware.bin": p.image} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	profiles, _ := json.Marshal(map[string]Profile{"normal": settings().Normal, "transfer": {Frequency: 434500000, Bandwidth: 500000, SF: 7, CR: 5, Power: 9}})
	os.WriteFile(filepath.Join(root, "profiles.json"), profiles, 0600)
	p.pauseFile = filepath.Join(root, "pause")
	args := []string{"--bridge", "SIMULATED-NO-USB", "--source", p.source, "--target", p.target, "--key-file", filepath.Join(root, "target.key"), "--counter-file", filepath.Join(root, "counter"), "--image", filepath.Join(root, "firmware.bin"), "--log", filepath.Join(root, "transfer.jsonl"), "--pause-file", p.pauseFile, "--windowed", "--block-size", "128", "--negotiate-profile", "--radio-profiles", filepath.Join(root, "profiles.json"), "--board-id", fmt.Sprintf("0x%08X", p.board), "--expected-version", "new"}
	if p.relay != "" {
		args = append(args, "--via", p.relay, "--route-key-file", filepath.Join(root, "route.key"))
	}
	return args
}
func TestNativeCompleteCycleWithoutPython(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // No external executables can be found.
	for _, board := range []uint32{0x54474732, 0x484c5633, 0x51524c50} {
		for _, relay := range []string{"", "F4MLV-10"} {
			t.Run(fmt.Sprintf("%x/%s", board, relay), func(t *testing.T) {
				p := newFakeOTA(board)
				p.relay = relay
				p.image = bytes.Repeat([]byte{0xa7}, 8500)
				p.image[0] = 0xe9
				args := nativeArgs(t, p)
				var out bytes.Buffer
				if err := runNative(args, &out, func(string) (otaPort, error) { return p, nil }, p.clock); err != nil {
					t.Fatal(err, out.String())
				}
				if !p.closed || !p.bridgeOff || p.pending || !bytes.Equal(p.received, p.image) || len(p.baud) != 2 || p.baud[1] != 115200 {
					t.Fatal("incomplete final state")
				}
				for _, event := range []string{"post_install_verified", "controller_service_restored", "radio_confirmed"} {
					if !strings.Contains(out.String(), event) {
						t.Fatal(event, out.String())
					}
				}
				if p.lastCounter < 1800000000 || p.manifestCount != 1 || len(p.dataOffsets) != 67 {
					t.Fatal(p.lastCounter, p.manifestCount, len(p.dataOffsets))
				}
			})
		}
	}
}
func TestNativeFailuresRecoveryAndPause(t *testing.T) {
	for _, fault := range []string{"hole", "format", "bitmap", "wrong-final-version", "restore", "pause", "activate-timeout"} {
		t.Run(fault, func(t *testing.T) {
			p := newFakeOTA(0x51524c50)
			p.relay = "F4MLV-10"
			p.fault = fault
			p.image = bytes.Repeat([]byte{0xa7}, 512)
			args := nativeArgs(t, p)
			var out bytes.Buffer
			err := runNative(args, &out, func(string) (otaPort, error) { return p, nil }, p.clock)
			if fault == "hole" || fault == "activate-timeout" {
				if err != nil {
					t.Fatal(err, out.String())
				}
			} else if err == nil {
				t.Fatal("failure accepted", out.String())
			}
			if fault == "hole" {
				if len(p.dataOffsets) != 5 || p.dataOffsets[4] != 128 {
					t.Fatal(p.dataOffsets)
				}
			}
			if fault == "format" {
				if !strings.Contains(out.String(), "protocol_rejected") || len(p.dataOffsets) != 0 {
					t.Fatal(out.String())
				}
			}
			if fault == "pause" {
				if !errors.Is(err, errOperatorPause) || !strings.Contains(out.String(), "operator_paused") {
					t.Fatal(err)
				}
			}
			if !p.closed || !p.bridgeOff {
				t.Fatal("port not restored/closed")
			}
		})
	}
}
func TestNativeBridgeErrorsAlwaysClose(t *testing.T) {
	for _, step := range []string{"mode", "timeout", "reset", "handshake"} {
		p := newFakeOTA(0x54474732)
		p.failOpenStep = step
		if _, err := openBridge("dummy", func(string) (otaPort, error) { return p, nil }, p.clock); err == nil {
			t.Fatal(step)
		}
		if !p.closed {
			t.Fatal("leaked serial port", step)
		}
	}
}
func TestNativeCounterMonotonicAndCorruption(t *testing.T) {
	p := newFakeOTA(0x54474732)
	path := filepath.Join(t.TempDir(), "counter")
	os.WriteFile(path, []byte("1900000000\n"), 0600)
	e := &nativeEngine{bridge: &bridge{port: p, clock: p.clock}, clock: p.clock, key: p.key, source: p.source, target: p.target, counterFile: path, responseTimeout: time.Second, normalTimeout: time.Second, event: func(string, map[string]any) {}}
	if _, err := e.hello(1); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.TrimSpace(string(b)) != "1900000001" {
		t.Fatal(string(b))
	}
	count := len(p.ops)
	os.WriteFile(path, []byte("corrupt"), 0600)
	if _, err := e.hello(1); err == nil || len(p.ops) != count {
		t.Fatal("corrupt counter used")
	}
	os.WriteFile(path, []byte(strconv.FormatUint(0xffffffff, 10)), 0600)
	if _, err := e.hello(1); err == nil {
		t.Fatal("counter wrapped")
	}
}
func TestNativeRollbackAndOffline(t *testing.T) {
	p := newFakeOTA(0x51524c50)
	p.image = bytes.Repeat([]byte{0xaa}, 512)
	p.relay = "F4MLV-10"
	p.clock.onSleep = func(d time.Duration) {
		if d == 190*time.Second && p.pending {
			p.running = 0x10000
			p.boot = 0x10000
			p.version = "old"
			p.pending = false
			p.rollback = true
		}
	}
	args := append(nativeArgs(t, p), "--exercise-confirm-timeout", "--require-offline")
	var out bytes.Buffer
	if err := runNative(args, &out, func(string) (otaPort, error) { return p, nil }, p.clock); err != nil {
		t.Fatal(err, out.String())
	}
	if !p.rollback || !strings.Contains(out.String(), "confirmation_timeout_rollback") || !strings.Contains(out.String(), "offline_after_update") {
		t.Fatal(out.String())
	}
}
func TestNativeRejectsForgedReplies(t *testing.T) {
	p := newFakeOTA(0x54474732)
	p.reply(6, 0, 71, nil)
	line, _ := p.rx.ReadString('\n')
	raw, _ := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(line, "RX ")))
	if _, err := decodeReply(p.key, p.source, p.target, raw); err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		bad := append([]byte{}, raw...)
		bad[i] ^= 1
		if _, err := decodeReply(p.key, p.source, p.target, bad); err == nil {
			t.Fatal("forgery accepted", i)
		}
	}
	if _, err := decodeReply(p.key, "OTHER", p.target, raw); err == nil {
		t.Fatal("wrong recipient accepted")
	}
}

var _ otaPort = (*fakeOTA)(nil)
var _ io.Writer = (*fakeOTA)(nil)

func TestNativeResumeAndLinkRecovery(t *testing.T) {
	for _, mode := range []string{"resume", "query-timeout"} {
		t.Run(mode, func(t *testing.T) {
			p := newFakeOTA(0x54474732)
			p.image = bytes.Repeat([]byte{0x45}, 8500)
			p.relay = "F4MLV-10"
			if mode == "resume" {
				p.received = make([]byte, len(p.image))
				copy(p.received, p.image[:4096])
				p.committed = 4096
				p.currentBase = 4096
			} else {
				p.fault = mode
			}
			args := nativeArgs(t, p)
			var out bytes.Buffer
			if err := runNative(args, &out, func(string) (otaPort, error) { return p, nil }, p.clock); err != nil {
				t.Fatal(err, out.String())
			}
			if mode == "resume" {
				for _, offset := range p.dataOffsets {
					if offset < 4096 {
						t.Fatal("resent durable page", offset)
					}
				}
			} else if !strings.Contains(out.String(), "link_recovered") {
				t.Fatal("recovery not exercised")
			}
			if !bytes.Equal(p.received, p.image) {
				t.Fatal("image mismatch")
			}
		})
	}
}

func TestNativeBridgeRecoversPreviousTransferBaud(t *testing.T) {
	p := newFakeOTA(0x54474732)
	p.deviceBaud = 921600
	b, err := openBridge("dummy", func(string) (otaPort, error) { return p, nil }, p.clock)
	if err != nil {
		t.Fatal(err)
	}
	defer b.port.Close()
	if p.hostBaud != 921600 || p.deviceBaud != 921600 {
		t.Fatal("baud not recovered")
	}
	if p.clock.Now().Sub(time.Unix(1800000000, 0)) < 15*time.Second {
		t.Fatal("initial baud timeout was not exercised")
	}
}
