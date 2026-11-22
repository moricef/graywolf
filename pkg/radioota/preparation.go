package radioota

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CommandSigner reuses Graywolf's credential store and persistent APRS counter.
// The signed identity is checked separately from the on-air recipient.
type CommandSigner func(recipient, signedTarget, controller, command string) (string, uint64, error)

type preparationConfig struct {
	Signer                    CommandSigner
	Recipient, RelayRecipient string
}

func controlReply(raw []byte, peer, controller string) (string, bool) {
	if len(raw) < 3 || !bytes.Equal(raw[:3], []byte{0x3c, 0xff, 1}) {
		return "", false
	}
	packet := string(raw[3:])
	gt := strings.IndexByte(packet, '>')
	colon := strings.IndexByte(packet, ':')
	if gt < 1 || colon < gt || packet[:gt] != peer {
		return "", false
	}
	info := packet[colon+1:]
	if len(info) < 11 || info[0] != ':' || info[10] != ':' || strings.TrimSpace(info[1:10]) != controller {
		return "", false
	}
	text := info[11:]
	if i := strings.IndexByte(text, '{'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSpace(text), true
}

// hop is one relay of a chain: its route identity (Callsign-SSID, the name it
// signs with and that the route frames carry) and its on-air APRS name, which
// may be a tactical callsign and is what an explicit APRS path must name.
type hop struct{ Call, RF string }

// relays lists the route from the controller towards the target: the relay
// next to the controller first. Empty for a direct target.
func (e *nativeEngine) relays() []hop {
	if e.relay == "" {
		return nil
	}
	rf := e.preparation.RelayRecipient
	if rf == "" {
		rf = e.relay
	}
	return append([]hop{{e.relay, rf}}, e.chain...)
}

// pathTo returns the on-air names of the relays before the n-th relay; n equal
// to the number of relays gives the full path to the target.
func (e *nativeEngine) pathTo(n int) []string {
	var path []string
	for _, r := range e.relays()[:n] {
		path = append(path, r.RF)
	}
	return path
}

func (e *nativeEngine) control(recipient, signedTarget, command string, path []string) (string, error) {
	if e.preparation.Signer == nil {
		return "", errors.New("authenticated route preparation is unavailable")
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err := e.maintainRendezvous(); err != nil {
			return "", err
		}
		if err := e.checkPause(); err != nil {
			return "", err
		}
		envelope, counter, err := e.preparation.Signer(recipient, signedTarget, e.source, command)
		if err != nil {
			return "", err
		}
		// Drop already buffered application replies. The firmware does not sign or
		// correlate its text reply, so readiness still requires a binary HMAC proof.
		if err = e.bridge.port.ResetInputBuffer(); err != nil {
			return "", err
		}
		e.bridge.buffer = nil
		// Each relay of an explicit path repeats the command in turn.
		digis := ""
		for _, name := range path {
			digis += "," + name
		}
		packet := fmt.Sprintf("%s>APGRWO%s,RFONLY::%-9s:%s", e.source, digis, recipient, envelope)
		wire := append([]byte{0x3c, 0xff, 1}, []byte(packet)...)
		if len(wire) > 255 {
			return "", errors.New("signed preparation packet exceeds LoRa frame size")
		}
		if err = e.bridge.write("TXRAW " + hex.EncodeToString(wire) + "\n"); err != nil {
			return "", err
		}
		e.emit("preparation_command", map[string]any{"recipient": recipient, "signed_target": signedTarget, "command": command, "counter": counter, "attempt": attempt})
		end := e.clock.Now().Add(e.normalTimeout)
		for e.clock.Now().Before(end) {
			if err = e.checkPause(); err != nil {
				return "", err
			}
			line, err := e.bridge.line()
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(line, "TXDONE ") && line != "TXDONE 0" {
				return "", errors.New("controller could not transmit the preparation command")
			}
			if !strings.HasPrefix(line, "RX ") {
				continue
			}
			raw, err := hex.DecodeString(strings.TrimPrefix(line, "RX "))
			if err != nil {
				continue
			}
			answer, ok := controlReply(raw, recipient, e.source)
			if !ok {
				// Record who else was heard (a relay repeating the command, another
				// station): the APRS header only, never the signed envelope.
				if len(raw) > 3 {
					if head, _, found := strings.Cut(string(raw[3:]), "::"); found && len(head) <= 80 && printableASCII(head) {
						e.emit("preparation_heard", map[string]any{"header": head, "command": command})
					}
				}
				continue
			}
			if strings.HasPrefix(answer, "ack") {
				continue
			}
			// ACKs are ignored. These are source-checked application observations,
			// never labelled authenticated evidence: the following HELLO is that proof.
			if !strings.HasPrefix(answer, "OTA ") && answer != "AUTH FAILED" {
				continue
			}
			if answer != "AUTH FAILED" && !strings.Contains(answer, "ERROR") && !strings.Contains(answer, "BAD ") && !strings.Contains(answer, "NO KEY") && answer != "OTA BUSY" {
				if command == "OTA=?" && !strings.HasPrefix(answer, "OTA NORMAL ") && !strings.HasPrefix(answer, "OTA FAST ") {
					continue
				}
				if strings.HasPrefix(command, "OTA=ROUTE") || strings.HasPrefix(command, "OTA=RELAY,") || strings.HasPrefix(command, "OTA=VIA,") {
					if !strings.HasPrefix(answer, "OTA ROUTE ") {
						continue
					}
				}
			}
			e.emit("preparation_reply", map[string]any{"recipient": recipient, "command": command, "text": answer, "authenticated": false})
			if answer == "AUTH FAILED" || strings.HasPrefix(answer, "OTA BUSY") || strings.Contains(answer, "ERROR") || strings.Contains(answer, "NO KEY") || strings.Contains(answer, "BAD ") {
				return "", fmt.Errorf("%s refused %s: %s", recipient, command, answer)
			}
			return answer, nil
		}
	}
	return "", fmt.Errorf("%w: no application reply from %s for %s; verify common service profile, RF message route and control credentials", errRadioTimeout, recipient, command)
}
func (e *nativeEngine) normalState(recipient, signed string, path []string) error {
	answer, err := e.control(recipient, signed, "OTA=?", path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(answer, "OTA NORMAL ARMED=0 PROFILE=") {
		return fmt.Errorf("%s is not idle on its normal profile: %s", recipient, answer)
	}
	return nil
}
func (e *nativeEngine) ensureRole(recipient, signed, command, expected string, path []string) error {
	actual, err := e.control(recipient, signed, "OTA=ROUTE?", path)
	if err != nil {
		return err
	}
	if actual == expected {
		return nil
	}
	if _, err = e.control(recipient, signed, command, path); err != nil {
		return err
	}
	actual, err = e.control(recipient, signed, "OTA=ROUTE?", path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%s route configuration was not established", recipient)
	}
	e.emit("route_role_configured", map[string]any{"recipient": recipient, "signed_target": signed, "role": expected})
	return nil
}

// relayRole is the command that makes the n-th relay forward between its two
// neighbours, and the route description that relay then reports. A single
// relay keeps the short form understood by every route firmware.
func (e *nativeEngine) relayRole(n int) (command, expected string) {
	chain := e.relays()
	previous, next := e.source, e.target
	if n > 0 {
		previous = chain[n-1].Call
	}
	if n < len(chain)-1 {
		next = chain[n+1].Call
	}
	if previous == e.source && next == e.target {
		return "OTA=RELAY," + e.target, fmt.Sprintf("OTA ROUTE RELAY %s>%s KEY=1", e.source, e.target)
	}
	return fmt.Sprintf("OTA=RELAY,%s,%s,%s", e.target, previous, next),
		fmt.Sprintf("OTA ROUTE RELAY %s>%s FROM %s TO %s KEY=1", e.source, e.target, previous, next)
}
func (e *nativeEngine) prepareRoute() error {
	e.emit("route_preparation_starting", nil)
	chain := e.relays()
	// Nearest relay first: each one is reached through the relays already
	// checked, and is configured before the next one depends on it.
	for n, r := range chain {
		path := e.pathTo(n)
		if err := e.normalState(r.RF, r.Call, path); err != nil {
			return err
		}
		command, expected := e.relayRole(n)
		if err := e.ensureRole(r.RF, r.Call, command, expected, path); err != nil {
			return err
		}
		if err := e.checkFastProfile(r.RF, r.Call, path); err != nil {
			return err
		}
	}
	if err := e.startRendezvous(); err != nil {
		return err
	}
	path := e.pathTo(len(chain))
	if err := e.normalState(e.preparation.Recipient, e.target, path); err != nil {
		return err
	}
	if len(chain) > 0 {
		last := chain[len(chain)-1].Call
		expected := fmt.Sprintf("OTA ROUTE TARGET VIA %s FROM %s KEY=1", last, e.source)
		if err := e.ensureRole(e.preparation.Recipient, e.target, "OTA=VIA,"+last, expected, path); err != nil {
			return err
		}
	} else {
		answer, err := e.control(e.preparation.Recipient, e.target, "OTA=ROUTE?", nil)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(answer, "OTA ROUTE OFF KEY=") {
			if _, err = e.control(e.preparation.Recipient, e.target, "OTA=ROUTE,OFF", nil); err != nil {
				return err
			}
			answer, err = e.control(e.preparation.Recipient, e.target, "OTA=ROUTE?", nil)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(answer, "OTA ROUTE OFF KEY=") {
				return errors.New("direct target route was not established")
			}
			e.emit("route_role_configured", map[string]any{"recipient": e.preparation.Recipient, "signed_target": e.target, "role": answer})
		}
	}
	if err := e.checkFastProfile(e.preparation.Recipient, e.target, path); err != nil {
		return err
	}
	e.emit("route_roles_observed", nil)
	return nil
}

func (e *nativeEngine) checkFastProfile(recipient, signed string, path []string) error {
	if e.transfer.SF >= 7 {
		return nil
	}
	p := e.transfer
	command := fmt.Sprintf("OTA=PROFILE,%d,%d,%d,%d,%d", p.Frequency, p.Bandwidth, p.SF, p.CR, p.Power)
	answer, err := e.control(recipient, signed, command, path)
	if err != nil {
		return err
	}
	if answer != "OTA PROFILE READY (RAM)" {
		return fmt.Errorf("%s did not accept the SF%d transfer profile: %s; SF5/SF6 require SX126x radios on the entire route", recipient, p.SF, answer)
	}
	e.emit("transfer_profile_observed", map[string]any{"recipient": recipient, "sf": p.SF, "authenticated": false})
	return nil
}
func (e *nativeEngine) releasePreflight() error {
	if e.rendezvousRelay != nil {
		if _, err := e.request(opRelease, 0, nil, e.normalTimeout, 1); err != nil {
			return err
		}
		state, err := e.radioState()
		if err != nil {
			return err
		}
		if state.Armed || state.RouteBusy || state.Service != e.normal || state.Applied != e.normal {
			return errors.New("target session release not verified")
		}
		e.emit("preflight_session_released", nil)
		return nil
	}
	// ABORT discards a candidate checkpoint. Do not use it to close a probe.
	// The route relay suppresses APRS forwarding while active, so wait for both
	// 60-second inactivity timers, without sending any keepalive or firmware data.
	e.emit("preflight_releasing_session", map[string]any{"seconds": 65})
	end := e.clock.Now().Add(65 * time.Second)
	for e.clock.Now().Before(end) {
		if err := e.checkPause(); err != nil {
			return err
		}
		if err := e.maintainRendezvous(); err != nil {
			return err
		}
		e.clock.Sleep(100 * time.Millisecond)
	}
	if e.rendezvousRelay == nil {
		for n, r := range e.relays() {
			if err := e.normalState(r.RF, r.Call, e.pathTo(n)); err != nil {
				return err
			}
		}
	}
	if err := e.normalState(e.preparation.Recipient, e.target, e.pathTo(len(e.relays()))); err != nil {
		return err
	}
	e.emit("preflight_session_released", nil)
	return nil
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
