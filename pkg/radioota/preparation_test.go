package radioota

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
)

type preparedPort struct {
	*fakeOTA
	roles             map[string]string
	rfTarget, rfRelay string
	counters          map[string]uint64
	commands          []string
	fault             string
	roleWrites        int
	armed             bool
	lastActivity      time.Time
}

func preparedFake(board uint32, relay string) *preparedPort {
	p := newFakeOTA(board)
	p.relay = relay
	p.image = bytes.Repeat([]byte{0x61}, 8500)
	p.image[0] = 0xe9
	p.image[12] = 0
	p.image[13] = 0
	if board != 0x54474732 {
		p.image[12] = 9
	}
	copy(p.image[32:36], []byte{0x32, 0x54, 0xcd, 0xab})
	return &preparedPort{fakeOTA: p, roles: map[string]string{}, rfTarget: "GENATX", rfRelay: "RELAYX", counters: map[string]uint64{}}
}
func (p *preparedPort) sign(rf, signed, controller, cmd string) (string, uint64, error) {
	want := p.target
	if rf == p.rfRelay {
		want = p.relay
	}
	if signed != want || controller != p.source {
		return "", 0, errors.New("wrong signing identities")
	}
	p.counters[rf]++
	envelope, err := remoteactions.BuildRemoteCommandEnvelope(base64.RawURLEncoding.EncodeToString(p.key), controller, signed, p.counters[rf], cmd)
	return envelope, p.counters[rf], err
}
func (p *preparedPort) Write(b []byte) (int, error) {
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "TXRAW ") {
		if strings.HasPrefix(line, "TX ") {
			raw, err := hex.DecodeString(strings.TrimPrefix(line, "TX "))
			if err != nil {
				return 0, err
			}
			if p.relay != "" {
				raw = raw[13 : len(raw)-16]
			}
			if raw[4] == 6 {
				p.armed = true
			}
			p.lastActivity = p.clock.Now()
			if raw[4] == 5 || raw[4] == 19 {
				p.armed = false
			}
			if p.fault == "hmac" && raw[4] == 6 {
				p.rx.WriteString("TXDONE 0\n")
				return len(b), nil
			}
		}
		return p.fakeOTA.Write(b)
	}
	p.rx.WriteString("TXDONE 0\n")
	raw, err := hex.DecodeString(strings.TrimPrefix(line, "TXRAW "))
	if err != nil {
		return 0, err
	}
	if !bytes.HasPrefix(raw, []byte{0x3c, 0xff, 1}) {
		return 0, errors.New("wrong APRS frame")
	}
	packet := string(raw[3:])
	colon := strings.IndexByte(packet, ':')
	info := packet[colon+1:]
	rf := strings.TrimSpace(info[1:10])
	envelope := info[11:]
	parts := strings.Split(envelope, ":")
	if len(parts) != 5 {
		return 0, errors.New("invalid envelope")
	}
	counter, err := strconv.ParseUint(parts[2], 36, 64)
	if err != nil {
		return 0, err
	}
	command := parts[3]
	signed := p.target
	if rf == p.rfRelay {
		signed = p.relay
	}
	expected, err := remoteactions.BuildRemoteCommandEnvelope(base64.RawURLEncoding.EncodeToString(p.key), p.source, signed, counter, command)
	if err != nil || expected != envelope {
		return 0, errors.New("bad APRS signature")
	}
	if rf == p.rfTarget && p.relay != "" && !strings.Contains(packet[:colon], ","+p.rfRelay+",") {
		return 0, errors.New("target APRS command omitted RF relay identity")
	}
	p.commands = append(p.commands, rf+":"+command)
	if p.clock.Now().Sub(p.lastActivity) > 60*time.Second {
		p.armed = false
	}
	answer := ""
	if p.fault == "ack-only" {
		answer = "ackAB"
	} else if p.fault == "reject" {
		answer = "AUTH FAILED"
	} else {
		switch {
		case command == "OTA=?":
			armed := 0
			if p.armed && rf == p.rfTarget {
				armed = 1
			}
			answer = fmt.Sprintf("OTA NORMAL ARMED=%d PROFILE=0", armed)
		case command == "OTA=ROUTE?":
			answer = p.roles[rf]
			if answer == "" {
				answer = "OTA ROUTE OFF KEY=1"
			}
		case strings.HasPrefix(command, "OTA=RELAY,"):
			p.roleWrites++
			answer = fmt.Sprintf("OTA ROUTE RELAY %s>%s KEY=1", p.source, p.target)
			p.roles[rf] = answer
		case strings.HasPrefix(command, "OTA=VIA,"):
			p.roleWrites++
			answer = fmt.Sprintf("OTA ROUTE TARGET VIA %s FROM %s KEY=1", p.relay, p.source)
			p.roles[rf] = answer
		case command == "OTA=ROUTE,OFF":
			p.roleWrites++
			p.roles[rf] = "OTA ROUTE OFF KEY=1"
			answer = p.roles[rf]
		default:
			return 0, fmt.Errorf("unexpected preparation command %s", command)
		}
	}
	// Stale/wrong source and APRS ACK must not satisfy a command.
	bad := fmt.Sprintf("OTHER>APRS::%-9s:OTA NORMAL ARMED=0 PROFILE=0", p.source)
	fmt.Fprintf(&p.rx, "RX %x\n", append([]byte{0x3c, 0xff, 1}, bad...))
	reply := fmt.Sprintf("%s>APLRG1::%-9s:%s", rf, p.source, answer)
	fmt.Fprintf(&p.rx, "RX %x\n", append([]byte{0x3c, 0xff, 1}, reply...))
	return len(b), nil
}
func (p *preparedPort) args(t *testing.T, preflight bool) []string {
	args := nativeArgs(t, p.fakeOTA)
	args = append(args, "--prepare-route", "--recipient", p.rfTarget, "--relay-recipient", p.rfRelay)
	if preflight {
		args = append(args, "--preflight-only")
	}
	return args
}
func TestPreparationProbePreservesCandidateAndRequiresHMAC(t *testing.T) {
	for _, relay := range []string{"", "F4MLV-10"} {
		t.Run(relay, func(t *testing.T) {
			p := preparedFake(0x51524c50, relay)
			p.committed = 4096
			p.received = make([]byte, len(p.image))
			copy(p.received, p.image[:4096])
			var out bytes.Buffer
			if err := runNative(p.args(t, true), &out, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign}); err != nil {
				t.Fatal(err, out.String())
			}
			if p.committed != 4096 || len(p.dataOffsets) != 0 || p.manifestCount != 0 {
				t.Fatal("preflight changed candidate")
			}
			for _, op := range p.ops {
				if op == 5 || op == 11 || op >= 15 {
					t.Fatal("preflight sent a destructive/transfer operation", op)
				}
			}
			for _, event := range []string{"route_authenticated", "preflight_session_released", "preflight_verified", "controller_service_restored"} {
				if !strings.Contains(out.String(), event) {
					t.Fatal(event, out.String())
				}
			}
			if relay != "" && p.roleWrites != 2 {
				t.Fatal("route roles were not prepared", p.commands)
			}
		})
	}
	p := preparedFake(0x54474732, "F4MLV-10")
	p.fault = "hmac"
	var out bytes.Buffer
	if err := runNative(p.args(t, true), &out, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign}); err == nil {
		t.Fatal("probe accepted no HMAC reply")
	}
	if strings.Contains(out.String(), "preflight_verified") || len(p.dataOffsets) != 0 {
		t.Fatal(out.String())
	}
}
func TestPreparationRejectsAckAndAuthenticationFailure(t *testing.T) {
	for _, fault := range []string{"ack-only", "reject"} {
		p := preparedFake(0x54474732, "F4MLV-10")
		p.fault = fault
		var out bytes.Buffer
		if err := runNative(p.args(t, true), &out, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign}); err == nil {
			t.Fatal(fault)
		}
		if len(p.ops) != 0 || p.roleWrites != 0 || !p.closed {
			t.Fatal("rejection did not stop before binary traffic")
		}
	}
}
func TestPreparationBeforeWholeTransfer(t *testing.T) {
	p := preparedFake(0x51524c50, "F4MLV-10")
	var out bytes.Buffer
	if err := runNative(p.args(t, false), &out, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign}); err != nil {
		t.Fatal(err, out.String())
	}
	if !bytes.Equal(p.received, p.image) || p.roleWrites != 2 || !strings.Contains(out.String(), "post_install_verified") {
		t.Fatal(out.String())
	}
	if strings.Index(out.String(), "route_authenticated") > strings.Index(out.String(), "page_committed") {
		t.Fatal("firmware sent before route proof")
	}
}
func TestPreparationServiceReadySurvivesRestart(t *testing.T) {
	p := preparedFake(0x54474732, "F4MLV-10")
	root := t.TempDir()
	var svc *Service
	svc = New(root, func(args []string, out io.Writer) error {
		return runNative(args, out, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
	})
	cfg := settings()
	cfg.Via = p.relay
	cfg.RelayRecipient = p.rfRelay
	cfg.RouteSecret = base64.RawURLEncoding.EncodeToString(p.routeKey)
	if err := svc.Save(p.rfTarget, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(p.rfTarget, p.source, p.target, base64.RawURLEncoding.EncodeToString(p.key), p.image, Options{ExpectedVersion: "new", PrepareRoute: true, PreflightOnly: true}); err != nil {
		t.Fatal(err)
	}
	j := wait(t, svc, p.rfTarget)
	if j.State != "ready" || !j.RouteAuthenticated || !j.SessionReleased || !j.PreflightVerified || j.PostInstallVerified {
		t.Fatal(j)
	}
	restored := New(root, nil).Status(p.rfTarget)
	if restored.State != "ready" {
		t.Fatal(restored)
	}
	if _, err := os.Stat(filepath.Join(root, "transfers", j.ID, "target.key")); !os.IsNotExist(err) {
		t.Fatal("temporary key retained")
	}
}
