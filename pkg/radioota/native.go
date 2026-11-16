package radioota

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

var errRadioTimeout = errors.New("radio response timeout")
var errOperatorPause = errors.New("operator paused the update")

type nativeEngine struct {
	preparation                                   preparationConfig
	prepareRouting, preflightOnly                 bool
	bridge                                        *bridge
	clock                                         engineClock
	key, routeKey                                 []byte
	source, target, relay                         string
	nonce                                         [8]byte
	normal, transfer                              Profile
	targetNormal                                  *Profile
	savedNormal                                   Profile
	relayKey                                      []byte
	chain                                         []hop // relays after the first one, towards the target
	rendezvousRelay                               *nativeEngine
	lastMaintain                                  time.Time
	maintainSequence                              uint32
	rendezvousMayHaveMoved                        bool
	rendezvousSuspended                           bool
	normalTimeout, responseTimeout, windowTimeout time.Duration
	lease                                         time.Duration
	fast, activationStarted, closing              bool
	counterFile, pauseFile                        string
	event                                         func(string, map[string]any)
	boardID                                       uint32
	retries                                       int
}

func (e *nativeEngine) emit(name string, fields map[string]any) { e.event(name, fields) }
func (e *nativeEngine) checkPause() error {
	if !e.activationStarted && !e.closing {
		if _, err := os.Stat(e.pauseFile); err == nil {
			return errOperatorPause
		}
	}
	return nil
}
func (e *nativeEngine) wire(op byte, offset uint32, payload []byte) ([]byte, error) {
	nonce := e.nonce
	if op == opHello {
		nonce = [8]byte{}
	}
	b, err := frame(e.key, e.source, e.target, nonce, op, offset, payload)
	if err != nil {
		return nil, err
	}
	if e.relay != "" {
		return wrapRoute(e.routeKey, e.source, b)
	}
	return b, nil
}
func (e *nativeEngine) request(op byte, offset uint32, payload []byte, timeout time.Duration, attempts int) (radioReply, error) {
	var zero radioReply
	if err := e.checkPause(); err != nil {
		return zero, err
	}
	if timeout == 0 {
		timeout = e.responseTimeout
	}
	wire, err := e.wire(op, offset, payload)
	if err != nil {
		return zero, err
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = e.maintainRendezvous(); err != nil {
			return zero, err
		}
		if err = e.checkPause(); err != nil {
			return zero, err
		}
		if err = e.bridge.write("TX " + hex.EncodeToString(wire) + "\n"); err != nil {
			return zero, err
		}
		e.emit("rf_tx", map[string]any{"target": e.target, "via": e.relay, "op": op, "offset": offset, "wire_bytes": len(wire), "attempt": attempt, "fast": e.fast})
		deadline := e.clock.Now().Add(timeout)
		for e.clock.Now().Before(deadline) {
			if err = e.checkPause(); err != nil {
				return zero, err
			}
			line, readErr := e.bridge.line()
			if readErr != nil {
				return zero, readErr
			}
			if strings.HasPrefix(line, "TXDONE ") {
				ok := line == "TXDONE 0"
				e.emit("rf_tx_done", map[string]any{"op": op, "offset": offset, "wire_bytes": len(wire), "attempt": attempt, "fast": e.fast, "ok": ok})
				if !ok {
					return zero, errors.New("controller radio transmission failed")
				}
				continue
			}
			if !strings.HasPrefix(line, "RX ") {
				continue
			}
			raw, decErr := hex.DecodeString(strings.TrimPrefix(line, "RX "))
			if decErr != nil {
				continue
			}
			if e.relay != "" {
				raw, decErr = unwrapRoute(e.routeKey, e.relay, raw)
				if decErr != nil {
					continue
				}
			}
			r, decErr := decodeReply(e.key, e.source, e.target, raw)
			if decErr != nil || r.Op != op || r.Offset != offset || (op != opHello && r.Nonce != e.nonce) {
				continue
			}
			e.emit("rf_reply", map[string]any{"target": e.target, "via": e.relay, "op": op, "offset": offset, "status": r.Status, "written": r.Written, "nonce": hex.EncodeToString(r.Nonce[:]), "bitmap": r.Bitmap, "has_bitmap": r.HasBitmap})
			if r.Status != 0 {
				e.emit("protocol_rejected", map[string]any{"op": op, "status": r.Status, "offset": offset, "payload_bytes": len(payload)})
				return zero, &protocolError{Op: op, Status: r.Status, Offset: offset}
			}
			if op == opHello {
				e.nonce = r.Nonce
			}
			return r, nil
		}
		e.retries++
		e.emit("retry", map[string]any{"op": op, "offset": offset, "attempt": attempt})
	}
	return zero, fmt.Errorf("%w: op=%d offset=%d", errRadioTimeout, op, offset)
}
func (e *nativeEngine) hello(attempts int) (radioReply, error) {
	previous := uint64(0)
	b, err := os.ReadFile(e.counterFile)
	if err == nil {
		previous, err = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32)
		if err != nil {
			return radioReply{}, fmt.Errorf("invalid saved OTA counter: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return radioReply{}, err
	}
	next := uint64(e.clock.Now().Unix())
	if next <= previous {
		next = previous + 1
	}
	if next == 0 || next > math.MaxUint32 {
		return radioReply{}, errors.New("OTA counter exhausted")
	}
	// write() fsyncs the file before atomic rename. Reserve before any radio TX.
	if err = write(e.counterFile, []byte(strconv.FormatUint(next, 10)+"\n")); err != nil {
		return radioReply{}, err
	}
	r, err := e.request(opHello, uint32(next), nil, e.normalTimeout, attempts)
	if err == nil {
		e.emit("hello", map[string]any{"target": e.target, "counter": next, "nonce": hex.EncodeToString(r.Nonce[:]), "reply": r.Proof, "written": r.Written})
	}
	return r, err
}
func (e *nativeEngine) configureProfiles() error {
	for i, p := range []Profile{e.normal, e.transfer} {
		command := fmt.Sprintf("PROFILEDEF %d %d %d %d %d %d", i, p.Frequency, p.Bandwidth, p.SF, p.CR, p.Power)
		if err := e.bridge.command(command, fmt.Sprintf("PROFILEDEF %d OK", i), 4*time.Second, time.Second); err != nil {
			return err
		}
	}
	e.normalTimeout = max(12*time.Second, 4*e.normal.airtime(255)+5*time.Second)
	// Every additional relay repeats each request and each answer once more.
	extra := time.Duration(len(e.chain))
	if e.relay != "" {
		e.normalTimeout = max(35*time.Second, e.normalTimeout) + extra*(2*e.normal.airtime(255)+5*time.Second)
	}
	burst := 32 * (e.transfer.airtime(208) + 20*time.Millisecond)
	e.windowTimeout = 20 * time.Second
	if e.relay != "" {
		// Each relay forwards the whole page from its cache before querying.
		e.windowTimeout = max(e.windowTimeout, (4+2*extra)*burst+(4+2*extra)*e.transfer.airtime(255)+5*time.Second)
	}
	e.emit("radio_profiles_configured", map[string]any{"normal": e.normal, "transfer": e.transfer})
	return e.bridgeProfile(false)
}
func (e *nativeEngine) bridgeProfile(fast bool) error {
	index := 0
	if fast {
		index = 1
	}
	if err := e.bridge.command(fmt.Sprintf("PROFILE %d", index), fmt.Sprintf("PROFILE %d OK", index), 3*time.Second, 500*time.Millisecond); err != nil {
		return err
	}
	e.fast = fast
	e.responseTimeout = e.normalTimeout
	if fast {
		e.responseTimeout = max(2*time.Second, 4*e.transfer.airtime(255)+2*time.Second) +
			time.Duration(len(e.chain))*(2*e.transfer.airtime(255)+time.Second)
	}
	return nil
}
func (e *nativeEngine) enterFast() error {
	e.lease = 60 * time.Second
	if 2*e.normal.airtime(255)+5*time.Second >= e.lease || e.transfer.airtime(255)+5*time.Second >= e.lease {
		return errors.New("radio profile is too slow for its lease")
	}
	if e.relay != "" && 32*(e.transfer.airtime(208)+20*time.Millisecond)+5*time.Second >= e.lease {
		return errors.New("page burst exceeds radio profile lease")
	}
	payload := make([]byte, 14)
	payload[0] = 2
	binary.BigEndian.PutUint16(payload[1:3], 60)
	p := e.transfer
	binary.BigEndian.PutUint32(payload[3:7], p.Frequency)
	binary.BigEndian.PutUint32(payload[7:11], p.Bandwidth)
	payload[11] = byte(p.SF)
	payload[12] = byte(p.CR)
	payload[13] = byte(int8(p.Power))
	if _, err := e.request(opPrepare, 0, payload, e.normalTimeout, 1); err != nil {
		if errors.Is(err, errRadioTimeout) {
			e.emit("prepare_ack_missing", nil)
			e.emit("waiting_for_profile_recovery", map[string]any{"seconds": 65})
			if err := e.wait(65 * time.Second); err != nil {
				return err
			}
		}
		return err
	}
	if err := e.bridgeProfile(true); err != nil {
		return err
	}
	e.clock.Sleep(2200 * time.Millisecond)
	if _, err := e.request(opStatus, 0, nil, 0, 8); err != nil {
		return err
	}
	e.emit("fast_profile_confirmed", map[string]any{"lease_seconds": 60})
	return nil
}
func (e *nativeEngine) returnNormal() error {
	_, err := e.request(opReturn, 0, nil, 0, 3)
	missing := errors.Is(err, errRadioTimeout)
	if err != nil && !missing {
		return err
	}
	if missing {
		e.emit("return_ack_missing", nil)
	}
	if err = e.bridgeProfile(false); err != nil {
		return err
	}
	if missing {
		e.emit("waiting_for_profile_recovery", map[string]any{"seconds": 65})
		if err := e.wait(65 * time.Second); err != nil {
			return err
		}
		if _, err = e.hello(3); err != nil {
			return err
		}
	} else {
		e.clock.Sleep(800 * time.Millisecond)
	}
	if _, err = e.request(opStatus, 0, nil, 0, 4); err != nil {
		return err
	}
	e.emit("normal_profile_confirmed", nil)
	return nil
}
func (e *nativeEngine) recover() error {
	if e.rendezvousRelay != nil {
		return e.recoverRendezvous()
	}
	if err := e.bridgeProfile(false); err != nil {
		return err
	}
	e.emit("waiting_for_profile_recovery", map[string]any{"seconds": 65})
	if err := e.wait(65 * time.Second); err != nil {
		return err
	}
	if _, err := e.hello(3); err != nil {
		return err
	}
	return e.enterFast()
}
func (e *nativeEngine) sendBlock(offset uint32, payload []byte) error {
	if err := e.maintainRendezvous(); err != nil {
		return err
	}
	if err := e.checkPause(); err != nil {
		return err
	}
	raw, err := e.wire(opWindowData, offset, payload)
	if err != nil {
		return err
	}
	if err = e.bridge.write("TX " + hex.EncodeToString(raw) + "\n"); err != nil {
		return err
	}
	e.emit("rf_tx", map[string]any{"op": opWindowData, "offset": offset, "wire_bytes": len(raw), "attempt": 1, "fast": e.fast})
	allowance := max(5*time.Second, e.transfer.airtime(255)+2*time.Second)
	end := e.clock.Now().Add(allowance)
	for e.clock.Now().Before(end) {
		if err = e.checkPause(); err != nil {
			return err
		}
		line, err := e.bridge.line()
		if err != nil {
			return err
		}
		if line == "TXDONE 0" {
			e.emit("rf_tx_done", map[string]any{"op": opWindowData, "offset": offset, "ok": true})
			e.clock.Sleep(5 * time.Millisecond)
			return nil
		}
		if strings.HasPrefix(line, "TXDONE ") || strings.HasPrefix(line, "ERROR") {
			return errors.New("bridge burst transmission failed")
		}
	}
	return fmt.Errorf("%w: burst transmission", errRadioTimeout)
}
func (e *nativeEngine) transferImage(image []byte, sha [32]byte) (radioReply, error) {
	manifest := make([]byte, 40)
	binary.BigEndian.PutUint32(manifest[:4], uint32(len(image)))
	copy(manifest[4:36], sha[:])
	binary.BigEndian.PutUint32(manifest[36:], e.boardID)
	initial, err := e.request(opWindowStart, 0, manifest, 12*time.Second, 16)
	if err != nil {
		return radioReply{}, err
	}
	position := initial.Written
	total := uint32(len(image))
	if position > total || (position != total && position%4096 != 0) {
		return radioReply{}, errors.New("invalid durable receiver offset")
	}
	e.emit("durable_progress", map[string]any{"written": position, "total": total})
	started := e.clock.Now()
	startOffset := position
	repairs, recoveries := 0, 0
	for position < total {
		length := min(uint32(4096), total-position)
		count := (length + 127) / 128
		mask := uint32((uint64(1) << count) - 1)
		missing := mask
		committed := false
		var pageErr error
		for attempt := 0; attempt < 16; attempt++ {
			for index := uint32(0); index < count; index++ {
				if missing&(1<<index) == 0 {
					continue
				}
				offset := position + index*128
				if pageErr = e.sendBlock(offset, image[offset:min(offset+128, position+length)]); pageErr != nil {
					break
				}
			}
			if pageErr != nil {
				break
			}
			query := make([]byte, 4)
			binary.BigEndian.PutUint32(query, mask)
			var reply radioReply
			reply, pageErr = e.request(opWindowQuery, position, query, e.windowTimeout, 2)
			if pageErr != nil {
				break
			}
			if !reply.HasBitmap || reply.Bitmap & ^mask != 0 || reply.Written > total {
				return radioReply{}, errors.New("invalid page bitmap")
			}
			if reply.Bitmap == mask && reply.Written == position+length {
				committed = true
				break
			}
			missing = mask & ^reply.Bitmap
			if missing == 0 {
				return radioReply{}, errors.New("complete bitmap without durable commit")
			}
			repairs += bits.OnesCount32(missing)
			e.emit("repair_page", map[string]any{"offset": position, "missing": missing, "attempt": attempt + 1})
		}
		if pageErr != nil {
			if !errors.Is(pageErr, errRadioTimeout) {
				return radioReply{}, pageErr
			}
			recoveries++
			if recoveries > 5 {
				return radioReply{}, pageErr
			}
			e.emit("link_lost", map[string]any{"durable": position, "recovery": recoveries})
			if err = e.recover(); err != nil {
				return radioReply{}, err
			}
			restored, err := e.request(opWindowStart, 0, manifest, 12*time.Second, 16)
			if err != nil {
				return radioReply{}, err
			}
			resumed := restored.Written
			if resumed < position || resumed > position+length || (resumed != total && resumed%4096 != 0) {
				return radioReply{}, errors.New("unexpected durable offset after link recovery")
			}
			position = resumed
			e.emit("link_recovered", map[string]any{"written": position})
			continue
		}
		if !committed {
			return radioReply{}, errors.New("page repair attempts exhausted")
		}
		position += length
		e.emit("page_committed", map[string]any{"written": position, "total": total})
	}
	result, err := e.request(opWindowFinish, total, nil, 12*time.Second, 16)
	if err != nil {
		return radioReply{}, err
	}
	if result.Written != total {
		return radioReply{}, errors.New("incomplete finished image")
	}
	e.emit("verified", map[string]any{"elapsed_s": e.clock.Now().Sub(started).Seconds(), "start_offset": startOffset, "repairs": repairs, "retries": e.retries, "reply": result.Proof})
	return result, nil
}
func (e *nativeEngine) close() error {
	e.closing = true
	var restoreErr error
	if e.fast {
		restoreErr = e.returnNormal()
		if restoreErr != nil {
			_ = e.bridgeProfile(false)
		}
	}
	if e.rendezvousRelay != nil {
		restoreErr = errors.Join(restoreErr, e.finishRendezvous())
	}
	offErr := e.bridge.command("BRIDGE OFF", "BRIDGE OFF OK", 2*time.Second, 500*time.Millisecond)
	var profileErr error
	if offErr == nil && e.targetNormal != nil && *e.targetNormal != e.savedNormal {
		// The controller resumes its own station duties as soon as the bridge
		// closes; a queued SF12 transmission can hold its console for seconds.
		profileErr = e.bridge.command("RADIOPROFILE", profileLine("RADIOPROFILE", e.savedNormal), 20*time.Second, time.Second)
	}
	// BRIDGE OFF restores RF but leaves the firmware UART at the transfer baud.
	// Restore its console explicitly so a subsequent non-resetting open works,
	// including when the profile proof above failed.
	if offErr == nil {
		// Same reason as RADIOPROFILE above: the controller may be transmitting.
		offErr = e.bridge.command("OTABRIDGE 115200", "OTABRIDGE OK", 20*time.Second, time.Second)
		if offErr == nil {
			offErr = e.bridge.port.SetMode(&serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit})
		}
	}
	closeErr := e.bridge.port.Close()
	offErr = errors.Join(offErr, profileErr)
	if offErr == nil {
		e.emit("controller_service_restored", nil)
	}
	if restoreErr != nil || offErr != nil {
		e.emit("restoration_unconfirmed", nil)
		return errors.Join(restoreErr, offErr, closeErr)
	}
	return closeErr
}
func requireBoot(r radioReply, what string) (*BootProof, error) {
	if r.Proof == nil || r.Proof.Pending == nil || r.Proof.Running == 0 || r.Proof.Boot == 0 {
		return nil, fmt.Errorf("missing boot proof: %s", what)
	}
	return r.Proof, nil
}
func (e *nativeEngine) offline(event string) error {
	r, err := e.request(opNetwork, 0, nil, 0, 16)
	if err != nil {
		return err
	}
	if !r.HasNetwork || r.WiFiMode != 0 || r.Connected || r.ActiveInterface {
		return errors.New("target is not offline")
	}
	e.emit(event, map[string]any{"wifi_mode": r.WiFiMode, "connected": r.Connected, "active_interface": r.ActiveInterface})
	return nil
}
func (e *nativeEngine) update(image []byte, opts Options) error {
	if err := e.configureProfiles(); err != nil {
		return err
	}
	if e.prepareRouting {
		if err := e.prepareRoute(); err != nil {
			message := "Could not prepare the selected radio route. No firmware blocks were sent. Check the saved radio profiles, RF recipients and control credentials; command details are retained on the Graywolf host."
			if e.targetNormal != nil && *e.targetNormal != e.savedNormal {
				message = "Could not establish the temporary radio rendezvous. No firmware blocks were sent. Different service profiles require coordination firmware v1 on all three devices. Check the profiles and retained command details on the Graywolf host."
			}
			e.emit("route_preparation_failed", map[string]any{"message": message})
			return err
		}
	}
	attempts := 80
	if e.prepareRouting {
		attempts = 3
	}
	beforeReply, err := e.hello(attempts)
	if err != nil {
		if e.prepareRouting {
			e.emit("route_preparation_failed", map[string]any{"message": "The end-to-end HMAC route check failed. No firmware blocks were sent. Check the route key, selected identities and common normal radio profile; no new key has been generated."})
		}
		return err
	}
	before, err := requireBoot(beforeReply, "initial")
	if err != nil {
		return err
	}
	if *before.Pending {
		return errors.New("target already has an unconfirmed candidate")
	}
	if e.rendezvousRelay != nil {
		if err := e.verifyTargetService(); err != nil {
			return err
		}
	}
	if e.prepareRouting {
		e.emit("route_authenticated", map[string]any{"reply": before, "written": beforeReply.Written})
	}
	if e.preflightOnly {
		if err := e.releasePreflight(); err != nil {
			return err
		}
		e.emit("preflight_verified", map[string]any{"reply": before})
		return nil
	}

	if opts.ReplaceExisting {
		if _, err = e.request(opAbort, 0, nil, 0, 16); err != nil {
			return err
		}
		e.emit("previous_candidate_discarded", nil)
		beforeReply, err = e.hello(80)
		if err != nil {
			return err
		}
		before, err = requireBoot(beforeReply, "after explicit discard")
		if err != nil {
			return err
		}
		if *before.Pending || beforeReply.Written != 0 {
			return errors.New("candidate discard not confirmed")
		}
	}
	if opts.RequireOffline {
		if err = e.offline("offline_before_update"); err != nil {
			return err
		}
	}
	if err = e.enterFast(); err != nil {
		return err
	}
	sha := sha256.Sum256(image)
	shaText := hex.EncodeToString(sha[:])
	e.emit("image", map[string]any{"bytes": len(image), "sha256": shaText, "board_id": e.boardID, "initial": before})
	verified, err := e.transferImage(image, sha)
	if err != nil {
		return err
	}
	proof, err := requireBoot(verified, "image verification")
	if err != nil {
		return err
	}
	if proof.Running != before.Running || proof.Boot != before.Boot {
		return errors.New("boot partition changed before activation")
	}
	if err = e.returnNormal(); err != nil {
		return err
	}
	if err = e.checkPause(); err != nil {
		return err
	}
	e.activationStarted = true
	e.emit("activation_starting", nil)
	_, err = e.request(opWindowActivate, uint32(len(image)), nil, e.normalTimeout, 3)
	if err != nil && !errors.Is(err, errRadioTimeout) {
		return err
	}
	if err != nil {
		e.emit("activation_ack_missing", nil)
	}
	e.clock.Sleep(5 * time.Second)
	newReply, err := e.hello(80)
	if err != nil {
		return err
	}
	candidate, err := requireBoot(newReply, "candidate")
	if err != nil {
		return err
	}
	validCandidate := func(p *BootProof) bool {
		return p != nil && p.Pending != nil && *p.Pending && p.Running != before.Running && p.Running == p.Boot && p.ImageSHA == shaText && p.Version == opts.ExpectedVersion
	}
	if !validCandidate(candidate) {
		return errors.New("candidate boot proof does not match the expected firmware")
	}
	e.emit("boot_pending_confirmation", map[string]any{"reply": candidate})
	// The boot proof above already identifies the candidate. Confirm it first:
	// the target's own 180-second rollback timer is running. The target's
	// service profile is verified after confirmation, before success.
	if opts.ExerciseConfirmTimeout {
		e.emit("confirmation_withheld", nil)
		if err = e.wait(190 * time.Second); err != nil {
			return err
		}
		fallbackReply, err := e.hello(80)
		if err != nil {
			return err
		}
		fallback, err := requireBoot(fallbackReply, "rollback")
		if err != nil {
			return err
		}
		if *fallback.Pending || fallback.Running != before.Running || fallback.Boot != before.Boot || fallback.Version != before.Version {
			return errors.New("automatic rollback not established")
		}
		e.emit("confirmation_timeout_rollback", map[string]any{"reply": fallback})
		_, err = e.request(opReactivate, 0, sha[:], e.normalTimeout, 3)
		if err != nil && !errors.Is(err, errRadioTimeout) {
			return err
		}
		if err != nil {
			e.emit("reactivation_ack_missing", nil)
		}
		e.clock.Sleep(5 * time.Second)
		newReply, err = e.hello(80)
		if err != nil {
			return err
		}
		candidate, err = requireBoot(newReply, "reactivated candidate")
		if err != nil {
			return err
		}
		if !validCandidate(candidate) {
			return errors.New("reactivated candidate proof mismatch")
		}
		e.emit("boot_pending_confirmation", map[string]any{"reply": candidate})
	}
	for i := 0; i < 2; i++ {
		confirmed, err := e.request(opConfirm, 0, sha[:], 0, 16)
		if err != nil {
			return err
		}
		p, err := requireBoot(confirmed, "confirmation")
		if err != nil {
			return err
		}
		if *p.Pending || p.Running != candidate.Running || p.Boot != candidate.Boot || p.Version != opts.ExpectedVersion || p.ImageSHA != shaText {
			return errors.New("confirmation proof mismatch")
		}
		if i == 1 {
			e.emit("radio_confirmed", map[string]any{"reply": p})
		}
	}
	if e.rendezvousRelay != nil {
		if err := e.verifyTargetService(); err != nil {
			return err
		}
	}
	if opts.RequireOffline {
		if err = e.offline("offline_after_update"); err != nil {
			return err
		}
	}
	// The image has been confirmed. This existing protocol closes the session
	// with ABORT; never use it for a non-destructive preflight.
	if _, err = e.request(opAbort, 0, nil, 0, 16); err != nil {
		return err
	}
	finalReply, err := e.hello(80)
	if err != nil {
		return err
	}
	final, err := requireBoot(finalReply, "fresh-session final readback")
	if err != nil {
		return err
	}
	if *final.Pending || final.Running != candidate.Running || final.Boot != candidate.Boot || final.Version != opts.ExpectedVersion || final.ImageSHA != shaText {
		return errors.New("fresh-session installation proof mismatch")
	}
	e.emit("post_install_verified", map[string]any{"reply": final})
	if e.rendezvousRelay != nil {
		if err := e.verifyTargetService(); err != nil {
			return err
		}
	}
	_, err = e.request(opAbort, 0, nil, 0, 16)
	return err
}

// nativeRunner uses only the Go serial implementation already shipped in
// Graywolf. Its arguments are internal job parameters, never a subprocess.
func nativeRunner(args []string, out io.Writer) error {
	return runNative(args, out, openOTAPort, wallClock{})
}
func runNative(args []string, out io.Writer, open func(string) (otaPort, error), clock engineClock, preparation ...preparationConfig) (retErr error) {
	fs := flag.NewFlagSet("radio-ota", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var port, source, target, keyFile, routeFile, relayKeyFile, relay, counter, imagePath, logPath, pause, profilesPath, boardText string
	fs.StringVar(&relayKeyFile, "relay-key-file", "", "")
	for name, dst := range map[string]*string{"bridge": &port, "source": &source, "target": &target, "key-file": &keyFile, "route-key-file": &routeFile, "via": &relay, "counter-file": &counter, "image": &imagePath, "log": &logPath, "pause-file": &pause, "radio-profiles": &profilesPath, "board-id": &boardText} {
		fs.StringVar(dst, name, "", "")
	}
	var windowed, negotiate, prepareRouting, preflightOnly bool
	var recipient, relayRecipient, chainText string
	var block int
	var opts Options
	fs.BoolVar(&prepareRouting, "prepare-route", false, "")
	fs.BoolVar(&preflightOnly, "preflight-only", false, "")
	fs.StringVar(&recipient, "recipient", "", "")
	fs.StringVar(&relayRecipient, "relay-recipient", "", "")
	fs.StringVar(&chainText, "chain", "", "")
	fs.BoolVar(&windowed, "windowed", false, "")
	fs.BoolVar(&negotiate, "negotiate-profile", false, "")
	fs.IntVar(&block, "block-size", 0, "")
	fs.StringVar(&opts.ExpectedVersion, "expected-version", "", "")
	fs.BoolVar(&opts.ReplaceExisting, "replace-existing", false, "")
	fs.BoolVar(&opts.RequireOffline, "require-offline", false, "")
	fs.BoolVar(&opts.ExerciseConfirmTimeout, "exercise-confirm-timeout", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !windowed || !negotiate || block != 128 {
		return errors.New("native OTA requires negotiated 128-byte window transfers")
	}
	if err := opts.Validate(); err != nil {
		return err
	}
	board, err := strconv.ParseUint(boardText, 0, 32)
	if err != nil || (board != 0x54474732 && board != 0x484c5633 && board != 0x51524c50) {
		return errors.New("invalid target board ID")
	}
	loadKey := func(path string) ([]byte, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(key) != 32 {
			return nil, errors.New("invalid OTA key file")
		}
		return key, nil
	}
	key, err := loadKey(keyFile)
	if err != nil {
		return err
	}
	var routeKey []byte
	if relay != "" {
		routeKey, err = loadKey(routeFile)
		if err != nil {
			return err
		}
	}
	var profiles struct {
		Normal       Profile  `json:"normal"`
		Transfer     Profile  `json:"transfer"`
		TargetNormal *Profile `json:"target_normal"`
	}
	b, err := os.ReadFile(profilesPath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &profiles); err != nil {
		return err
	}
	if !profiles.Normal.Valid() || !profiles.Transfer.Valid() {
		return errors.New("invalid radio profiles")
	}
	chain, err := parseChain(chainText, source, target, relay)
	if err != nil {
		return err
	}
	if len(chain) > 0 && (relay == "" || !prepareRouting) {
		return errors.New("a relay chain requires a first relay and route preparation")
	}
	var relayKey []byte
	if profiles.TargetNormal != nil && *profiles.TargetNormal != profiles.Normal {
		if !profiles.TargetNormal.Valid() || relay == "" || !prepareRouting {
			return errors.New("different target service profile requires relay route preparation")
		}
		if len(chain) > 0 {
			return errors.New("different service profiles are coordinated through a single relay only")
		}
		relayKey, err = loadKey(relayKeyFile)
		if err != nil {
			return err
		}
	}
	image, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	// Service.Start has already validated the selected chip and image.
	if len(image) < 256 || len(image) > MaxImage {
		return errors.New("invalid firmware size")
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	encoder := json.NewEncoder(io.MultiWriter(out, log))
	emit := func(name string, fields map[string]any) {
		event := map[string]any{"time": float64(clock.Now().UnixMilli()) / 1000, "event": name}
		for k, v := range fields {
			event[k] = v
		}
		_ = encoder.Encode(event)
	}
	var prep preparationConfig
	if len(preparation) > 0 {
		prep = preparation[0]
	}
	prep.Recipient = recipient
	prep.RelayRecipient = relayRecipient
	if prepareRouting && (prep.Signer == nil || recipient == "" || relay != "" && relayRecipient == "") {
		return errors.New("route preparation requires stored control credentials and RF recipients")
	}
	if preflightOnly && !prepareRouting {
		return errors.New("preflight requires route preparation")
	}
	br, err := openBridge(port, open, clock)
	if err != nil {
		emit("engine_failed", map[string]any{"error": err.Error()})
		return err
	}
	e := &nativeEngine{preparation: prep, prepareRouting: prepareRouting, preflightOnly: preflightOnly, bridge: br, clock: clock, key: key, routeKey: routeKey, source: source, target: target, relay: relay, normal: profiles.Normal, transfer: profiles.Transfer, counterFile: counter, pauseFile: pause, event: emit, boardID: uint32(board)}
	e.savedNormal = profiles.Normal
	e.targetNormal = profiles.TargetNormal
	e.relayKey = relayKey
	e.chain = chain
	defer func() {
		closeErr := e.close()
		retErr = errors.Join(retErr, closeErr)
		if retErr != nil && (!errors.Is(retErr, errOperatorPause) || closeErr != nil) {
			e.emit("engine_failed", map[string]any{"error": retErr.Error()})
		}
	}()
	if relay != "" {
		route := []string{relay}
		for _, r := range chain {
			route = append(route, r.Call)
		}
		e.emit("route_selected", map[string]any{"via": relay, "relays": route, "chunk": 175})
	}
	err = e.update(image, opts)
	if errors.Is(err, errOperatorPause) {
		e.emit("operator_paused", nil)
	}
	return err
}

// MaxChain bounds the relays after the first one. Every relay adds a full
// repetition of each page and each answer to a transfer limited to two hours.
const MaxChain = 3

// parseChain reads "CALL[:RFNAME],CALL[:RFNAME]" for the relays after the
// first one, nearest first. Every station appears once on the route.
func parseChain(text, source, target, first string) ([]hop, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	seen := map[string]bool{source: true, target: true, first: true}
	var chain []hop
	for _, item := range strings.Split(text, ",") {
		call, rf, found := strings.Cut(strings.TrimSpace(item), ":")
		if !found {
			rf = call
		}
		if !address.MatchString(call) || !rfRecipient.MatchString(rf) {
			return nil, fmt.Errorf("invalid relay %q: use CALL-SSID or CALL-SSID:RFNAME", item)
		}
		if seen[call] {
			return nil, fmt.Errorf("relay %s appears twice on the route", call)
		}
		seen[call] = true
		chain = append(chain, hop{call, rf})
	}
	if len(chain) > MaxChain {
		return nil, fmt.Errorf("at most %d relays after the first one", MaxChain)
	}
	return chain, nil
}
