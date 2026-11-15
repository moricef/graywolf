package radioota

import (
	"bytes"
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This peer checks addresses, HMAC and actual tuning before delivering a frame.
// Its lease is independent of target traffic and of the target rollback timer.
type rendezvousPort struct {
	*preparedPort
	service, targetService, current     Profile
	definitions                         [2]Profile
	relayKey                            []byte
	relayNonce                          [8]byte
	relayArmed, fast, entered, released bool
	phase                               byte
	switchAt, activity, started         time.Time
	seq                                 uint32
	keepCount                           int
	fault                               string
	slowData                            bool
}

func (p *rendezvousPort) poll() {
	now := p.clock.Now()
	if p.phase == 1 && !now.Before(p.switchAt) {
		p.phase = 2
		p.activity = now
	}
	if p.phase == 3 && !now.Before(p.switchAt) || p.phase == 2 && (now.Sub(p.activity) >= 60*time.Second || now.Sub(p.started) >= 2*time.Hour) {
		p.phase = 0
		p.relayArmed = false
		p.fast = false
	}
}
func (p *rendezvousPort) effective() Profile {
	if p.fast {
		return p.definitions[1]
	}
	if p.phase == 2 || p.phase == 3 {
		return p.targetService
	}
	return p.service
}
func independentProfile(p Profile) []byte {
	b := make([]byte, 11)
	binary.BigEndian.PutUint32(b, p.Frequency)
	binary.BigEndian.PutUint32(b[4:], p.Bandwidth)
	b[8] = byte(p.SF)
	b[9] = byte(p.CR)
	b[10] = byte(p.Power)
	return b
}
func (p *rendezvousPort) coordinationReply(raw []byte, key []byte, service, applied Profile, phase byte, armed, routed bool, status byte) {
	payload := make([]byte, 37)
	payload[0] = raw[4]
	payload[1] = status
	payload[6] = 1
	payload[7] = phase
	if armed {
		payload[8] = 1
	}
	payload[10] = 1
	copy(payload[11:], independentProfile(service))
	copy(payload[22:], independentProfile(applied))
	binary.BigEndian.PutUint32(payload[33:], p.seq)
	if raw[4] == opHello {
		payload = payload[:6]
	}
	out := []byte{0x3c, 0xff, 0x7e, 2, 128}
	out = append(out, raw[5:17]...)
	out = append(out, 0, byte(len(payload)))
	out = append(out, raw[27:35]...)
	out = append(out, raw[19:27]...)
	out = append(out, payload...)
	out = append(out, independentTag(key, "LORA-APRS-OTA2", out)...)
	if routed {
		hop := []byte{0x3c, 0xff, 0x7d, 1, 3}
		hop = append(hop, identity(p.relay)...)
		hop = append(hop, out...)
		out = append(hop, independentTag(p.routeKey, "LORA-APRS-OTA-HOP1", hop)...)
	}
	fmt.Fprintf(&p.rx, "RX %x\n", out)
}
func (p *rendezvousPort) Write(b []byte) (int, error) {
	p.poll()
	line := strings.TrimSpace(string(b))
	if strings.HasPrefix(line, "PROFILEDEF ") {
		var index int
		var q Profile
		if _, err := fmt.Sscanf(line, "PROFILEDEF %d %d %d %d %d %d", &index, &q.Frequency, &q.Bandwidth, &q.SF, &q.CR, &q.Power); err != nil {
			return 0, err
		}
		p.definitions[index] = q
	}
	if strings.HasPrefix(line, "PROFILE ") {
		i, _ := strconv.Atoi(strings.TrimPrefix(line, "PROFILE "))
		p.current = p.definitions[i]
	}
	if line == "RADIOPROFILE" {
		fmt.Fprintln(&p.rx, profileLine("RADIOPROFILE", p.current))
		return len(b), nil
	}
	if line == "RADIOSERVICE" {
		fmt.Fprintln(&p.rx, profileLine("RADIOSERVICE", p.service))
		return len(b), nil
	}
	if line == "BRIDGE OFF" {
		p.current = p.service
	}
	if strings.HasPrefix(line, "TX ") {
		raw, err := hex.DecodeString(strings.TrimPrefix(line, "TX "))
		if err != nil {
			return 0, err
		}
		routed := len(raw) > 4 && raw[2] == 0x7d
		inner := raw
		if routed {
			inner = raw[13 : len(raw)-16]
		}
		if !routed && bytes.Equal(inner[27:35], identity(p.relay)) {
			p.rx.WriteString("TXDONE 0\n")
			if !hmac.Equal(inner[len(inner)-16:], independentTag(p.relayKey, "LORA-APRS-OTA2", inner[:len(inner)-16])) {
				return len(b), nil
			}
			if p.current != p.effective() {
				return len(b), nil
			}
			op := inner[4]
			offset := binary.BigEndian.Uint32(inner[13:17])
			status := byte(0)
			if op == opHello {
				p.relayArmed = true
				p.relayNonce = [8]byte{7, 6, 5, 4, 3, 2, 1, 9}
				tmp := append([]byte{}, inner...)
				copy(tmp[5:13], p.relayNonce[:])
				p.coordinationReply(tmp, p.relayKey, p.service, p.effective(), p.phase, true, false, 0)
				return len(b), nil
			}
			if op != opRadioState && (!p.relayArmed || !bytes.Equal(inner[5:13], p.relayNonce[:])) {
				return len(b), nil
			}
			if op == opRendezvous {
				if p.phase != 0 {
					status = 2
				} else {
					p.phase = 1
					p.switchAt = p.clock.Now().Add(2 * time.Second)
					p.started = p.clock.Now()
					p.entered = true
				}
			}
			if p.fault == "relay-silent" {
				if p.phase == 2 {
					return len(b), nil
				}
				p.fault = ""
			}
			if op == opMaintain && p.fault == "lose-relay" && p.keepCount >= 2 {
				p.fault = "relay-silent"
				return len(b), nil
			}
			if op == opMaintain {
				if p.phase != 2 || offset <= p.seq {
					status = 2
				} else {
					p.seq = offset
					p.activity = p.clock.Now()
					p.keepCount++
				}
			}
			if op == opMaintain && p.fault == "lose-keepalive-once" && p.keepCount == 2 {
				p.fault = ""
				return len(b), nil
			}
			if op == opRelease {
				p.phase = 3
				p.switchAt = p.clock.Now().Add(2 * time.Second)
				p.relayArmed = false
				p.released = true
			}
			if p.fault == "missing-enter-ack" && op == opRendezvous {
				return len(b), nil
			}
			if p.fault == "bad-state" && op == opRadioState {
				p.coordinationReply(inner, p.relayKey, p.service, p.targetService, p.phase, p.relayArmed, false, status)
				return len(b), nil
			}
			p.coordinationReply(inner, p.relayKey, p.service, p.effective(), p.phase, p.relayArmed, false, status)
			return len(b), nil
		}
		if routed {
			if p.phase != 2 || p.current != p.effective() {
				p.rx.WriteString("TXDONE 0\n")
				return len(b), nil
			}
			if inner[4] == opRadioState || inner[4] == opRelease {
				if !hmac.Equal(inner[len(inner)-16:], independentTag(p.key, "LORA-APRS-OTA2", inner[:len(inner)-16])) {
					return 0, fmt.Errorf("invalid target state request")
				}
				p.rx.WriteString("TXDONE 0\n")
				if inner[4] == opRelease {
					p.armed = false
				}
				p.coordinationReply(inner, p.key, p.targetService, p.targetService, 0, p.armed, true, 0)
				return len(b), nil
			}
			if inner[4] == opWindowData && p.slowData {
				// Let simulated time pass during a page so keepalives fall due.
				p.clock.at = p.clock.at.Add(2 * time.Second)
				p.poll()
			}
			if inner[4] == opPrepare {
				p.fast = true
			}
			if inner[4] == opReturn {
				p.fast = false
			}
		}
	}
	if strings.HasPrefix(line, "TXRAW ") && p.phase == 2 && p.current != p.targetService {
		return 0, fmt.Errorf("control on wrong frequency")
	}
	return p.preparedPort.Write(b)
}
func differentProfiles(t *testing.T) (*rendezvousPort, []string, string) {
	t.Helper()
	base := preparedFake(0x54474732, "F4MLV-10")
	base.rfRelay = "F4MLV-10"
	p := &rendezvousPort{preparedPort: base, service: settings().Normal, relayKey: bytes.Repeat([]byte{0xa5}, 32)}
	p.targetService = p.service
	p.targetService.Frequency = 434100000
	p.current = p.service
	args := nativeArgs(t, base.fakeOTA)
	root := filepath.Dir(base.pauseFile)
	profiles, _ := json.Marshal(map[string]Profile{"normal": p.service, "transfer": {434500000, 500000, 7, 5, 9}, "target_normal": p.targetService})
	if err := os.WriteFile(filepath.Join(root, "profiles.json"), profiles, 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "relay.key")
	if err := os.WriteFile(keyPath, []byte(base64.RawURLEncoding.EncodeToString(p.relayKey)), 0600); err != nil {
		t.Fatal(err)
	}
	args = append(args, "--prepare-route", "--recipient", p.rfTarget, "--relay-recipient", p.rfRelay, "--relay-key-file", keyPath)
	return p, args, root
}
func TestDifferentServicePreflight(t *testing.T) {
	p, args, _ := differentProfiles(t)
	args = append(args, "--preflight-only")
	var log bytes.Buffer
	err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if !p.entered || !p.released || p.phase != 0 || p.current != p.service || p.manifestCount != 0 {
		t.Fatalf("preflight did not restore: %+v", p)
	}
	for _, op := range p.ops {
		if op == opAbort || op == opWindowData || op == opWindowStart {
			t.Fatalf("destructive preflight op %d", op)
		}
	}
	if !strings.Contains(log.String(), "relay_service_restored") {
		t.Fatal(log.String())
	}
}
func TestDifferentServiceFailureRestores(t *testing.T) {
	for _, fault := range []string{"missing-enter-ack", "bad-state"} {
		t.Run(fault, func(t *testing.T) {
			p, args, _ := differentProfiles(t)
			p.fault = fault
			var log bytes.Buffer
			err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
			if err == nil {
				t.Fatal("unsafe rendezvous accepted")
			}
			if p.manifestCount != 0 || p.current != p.service {
				t.Fatal("firmware sent or controller not restored")
			}
			p.poll()
			if p.phase != 0 {
				t.Fatal("relay lease did not restore")
			}
		})
	}
}
func TestCoordinationRejectsTamperedProof(t *testing.T) {
	p, _, _ := differentProfiles(t)
	raw, _ := frame(p.key, p.source, p.target, [8]byte{1}, opRadioState, 0, nil)
	p.coordinationReply(raw, p.key, p.targetService, p.targetService, 0, false, false, 0)
	wire, _ := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(p.rx.String(), "RX ")))
	if _, err := decodeReply(p.key, p.source, p.target, wire); err != nil {
		t.Fatal(err)
	}
	for i := range wire {
		bad := append([]byte{}, wire...)
		bad[i] ^= 1
		if _, err := decodeReply(p.key, p.source, p.target, bad); err == nil {
			t.Fatalf("accepted mutation at %d", i)
		}
	}
}
func TestFirmwareCoordinationVector(t *testing.T) {
	// Produced by the firmware's encodeCoordinationState, not the Go encoder.
	b, err := os.ReadFile("testdata/rendezvous-state.hex")
	if err != nil {
		t.Fatal(err)
	}
	state, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(state) != 31 {
		t.Fatal("invalid firmware vector")
	}
	key := bytes.Repeat([]byte{0x32}, 32)
	payload := append([]byte{32, 0, 0, 0, 0, 0}, state...)
	raw := []byte{0x3c, 0xff, 0x7e, 2, 128, 1, 2, 3, 4, 5, 6, 7, 8, 0, 0, 0, 0, 0, 37}
	raw = append(raw, identity("F4MLV-10")...)
	raw = append(raw, identity("F4MLV-2")...)
	raw = append(raw, payload...)
	raw = append(raw, independentTag(key, "LORA-APRS-OTA2", raw)...)
	r, err := decodeReply(key, "F4MLV-2", "F4MLV-10", raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Radio == nil || r.Radio.Service != (Profile{433775000, 125000, 12, 5, 20}) || r.Radio.Applied != (Profile{434100000, 125000, 12, 5, 20}) || r.Radio.Sequence != 17 || r.Radio.Phase != 2 || !r.Radio.Armed || !r.Radio.Known {
		t.Fatalf("firmware proof misread: %+v", r.Radio)
	}
}

func TestDifferentServiceCompleteCycle(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprint(rollback), func(t *testing.T) {
			p, args, _ := differentProfiles(t)
			if rollback {
				args = append(args, "--exercise-confirm-timeout", "--require-offline")
				var pendingAt time.Time
				p.clock.onSleep = func(time.Duration) {
					p.poll()
					if p.pending && pendingAt.IsZero() {
						pendingAt = p.clock.Now()
					}
					if p.pending && !p.rollback && p.clock.Now().Sub(pendingAt) >= 180*time.Second {
						p.running = 0x10000
						p.boot = 0x10000
						p.version = "old"
						p.pending = false
						p.rollback = true
					}
				}
			}
			var log bytes.Buffer
			err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
			if err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			if p.manifestCount != 1 || p.phase != 0 || p.current != p.service || !p.released || p.pending {
				t.Fatal("cycle or restoration incomplete")
			}
			// The 190-second withheld confirmation alone spans seven keepalive intervals.
			if rollback && (!p.rollback || p.keepCount < int(190*time.Second/keepaliveInterval)) {
				t.Fatal("relay not maintained during independent target rollback")
			}
			for _, event := range []string{"post_install_verified", "relay_service_restored", "controller_service_restored"} {
				if !strings.Contains(log.String(), event) {
					t.Fatalf("missing %s", event)
				}
			}
			// Nothing but the boot proof may stand between the candidate's
			// boot and its confirmation: the target's rollback timer runs.
			events := eventsOnly(log.String())
			boot := strings.LastIndex(events, "boot_pending_confirmation")
			if confirmed := strings.Index(events[boot:], "radio_confirmed"); confirmed < 0 || strings.Contains(events[boot:boot+confirmed], "target_service_verified") {
				t.Fatalf("confirmation delayed by other checks: %s", events[boot:])
			}
		})
	}
}

// A single lost keepalive answer must not end the transfer, and a relay that
// stays silent past its lease must be brought back by a new rendezvous.
func TestDifferentServiceKeepaliveLoss(t *testing.T) {
	for _, fault := range []string{"lose-keepalive-once", "lose-relay"} {
		t.Run(fault, func(t *testing.T) {
			p, args, _ := differentProfiles(t)
			p.fault = fault
			p.slowData = true
			var log bytes.Buffer
			err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
			if err != nil {
				t.Fatalf("%v\n%s", err, log.String())
			}
			if p.phase != 0 || p.current != p.service || !p.released || p.pending || p.fault != "" {
				t.Fatalf("cycle or restoration incomplete (fault %q, keepalives %d)\n%s", p.fault, p.keepCount, eventsOnly(log.String()))
			}
			established := strings.Count(log.String(), "rendezvous_established")
			if fault == "lose-keepalive-once" && (established != 1 || strings.Contains(log.String(), "link_lost") || !strings.Contains(log.String(), "rendezvous_keepalive_retry")) {
				t.Fatalf("one lost answer was not absorbed by a retry\n%s", log.String())
			}
			if fault == "lose-relay" && (established != 2 || !strings.Contains(log.String(), "link_recovered")) {
				t.Fatalf("rendezvous not established again after lease loss (%d)\n%s", established, log.String())
			}
			for _, event := range []string{"post_install_verified", "relay_service_restored", "controller_service_restored"} {
				if !strings.Contains(log.String(), event) {
					t.Fatalf("missing %s", event)
				}
			}
		})
	}
}

var _ io.Writer = (*rendezvousPort)(nil)

func eventsOnly(log string) string {
	var out []string
	for _, line := range strings.Split(log, "\n") {
		if i := strings.Index(line, `"event":"`); i >= 0 && !strings.Contains(line, `"rf_`) {
			rest := line[i+9:]
			if j := strings.Index(rest, `"`); j >= 0 {
				out = append(out, rest[:j])
			}
		}
	}
	return strings.Join(out, " ")
}
