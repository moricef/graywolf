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

func (e *nativeEngine) control(recipient, signedTarget, command string, via bool) (string, error) {
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
		path := ""
		if via && e.relay != "" {
			path = "," + e.preparation.RelayRecipient
		}
		packet := fmt.Sprintf("%s>APGRWO%s,RFONLY::%-9s:%s", e.source, path, recipient, envelope)
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
func (e *nativeEngine) normalState(recipient, signed string, via bool) error {
	answer, err := e.control(recipient, signed, "OTA=?", via)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(answer, "OTA NORMAL ARMED=0 PROFILE=") {
		return fmt.Errorf("%s is not idle on its normal profile: %s", recipient, answer)
	}
	return nil
}
func (e *nativeEngine) ensureRole(recipient, signed, command, expected string, via bool) error {
	actual, err := e.control(recipient, signed, "OTA=ROUTE?", via)
	if err != nil {
		return err
	}
	if actual == expected {
		return nil
	}
	if _, err = e.control(recipient, signed, command, via); err != nil {
		return err
	}
	actual, err = e.control(recipient, signed, "OTA=ROUTE?", via)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%s route configuration was not established", recipient)
	}
	e.emit("route_role_configured", map[string]any{"recipient": recipient, "signed_target": signed, "role": expected})
	return nil
}
func (e *nativeEngine) prepareRoute() error {
	e.emit("route_preparation_starting", nil)
	if e.relay != "" {
		rf := e.preparation.RelayRecipient
		if err := e.normalState(rf, e.relay, false); err != nil {
			return err
		}
		expected := fmt.Sprintf("OTA ROUTE RELAY %s>%s KEY=1", e.source, e.target)
		if err := e.ensureRole(rf, e.relay, "OTA=RELAY,"+e.target, expected, false); err != nil {
			return err
		}
	}
	if err := e.startRendezvous(); err != nil {
		return err
	}
	if err := e.normalState(e.preparation.Recipient, e.target, e.relay != ""); err != nil {
		return err
	}
	if e.relay != "" {
		expected := fmt.Sprintf("OTA ROUTE TARGET VIA %s FROM %s KEY=1", e.relay, e.source)
		if err := e.ensureRole(e.preparation.Recipient, e.target, "OTA=VIA,"+e.relay, expected, true); err != nil {
			return err
		}
	} else {
		answer, err := e.control(e.preparation.Recipient, e.target, "OTA=ROUTE?", false)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(answer, "OTA ROUTE OFF KEY=") {
			if _, err = e.control(e.preparation.Recipient, e.target, "OTA=ROUTE,OFF", false); err != nil {
				return err
			}
			answer, err = e.control(e.preparation.Recipient, e.target, "OTA=ROUTE?", false)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(answer, "OTA ROUTE OFF KEY=") {
				return errors.New("direct target route was not established")
			}
			e.emit("route_role_configured", map[string]any{"recipient": e.preparation.Recipient, "signed_target": e.target, "role": answer})
		}
	}
	e.emit("route_roles_observed", nil)
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
	if e.relay != "" && e.rendezvousRelay == nil {
		if err := e.normalState(e.preparation.RelayRecipient, e.relay, false); err != nil {
			return err
		}
	}
	if err := e.normalState(e.preparation.Recipient, e.target, e.relay != ""); err != nil {
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
