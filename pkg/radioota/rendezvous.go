package radioota

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type radioState struct {
	Phase                   byte
	Armed, RouteBusy, Known bool
	Service, Applied        Profile
	Sequence                uint32
}

func decodeProfile(b []byte) Profile {
	return Profile{binary.BigEndian.Uint32(b[:4]), binary.BigEndian.Uint32(b[4:8]), int(b[8]), int(b[9]), int(int8(b[10]))}
}
func encodeProfile(p Profile) []byte {
	b := make([]byte, 11)
	binary.BigEndian.PutUint32(b, p.Frequency)
	binary.BigEndian.PutUint32(b[4:], p.Bandwidth)
	b[8] = byte(p.SF)
	b[9] = byte(p.CR)
	b[10] = byte(int8(p.Power))
	return b
}
func (e *nativeEngine) radioState() (*radioState, error) {
	old := e.nonce
	defer func() { e.nonce = old }()
	if _, err := rand.Read(e.nonce[:]); err != nil {
		return nil, err
	}
	r, err := e.request(opRadioState, 0, nil, e.normalTimeout, 1)
	if err != nil {
		return nil, fmt.Errorf("could not verify coordination v1 for %s: %w", e.target, err)
	}
	if r.Radio == nil || !r.Radio.Known {
		return nil, errors.New("effective radio profile is unknown")
	}
	return r.Radio, nil
}
func (e *nativeEngine) maintainRendezvous() error {
	relay := e.rendezvousRelay
	if relay == nil || e.rendezvousSuspended || e.clock.Now().Sub(e.lastMaintain) < 10*time.Second {
		return nil
	}
	var r radioReply
	var err error
	for attempt := 1; ; attempt++ {
		e.maintainSequence++
		if r, err = relay.request(opMaintain, e.maintainSequence, nil, e.responseTimeout, 1); err == nil {
			break
		}
		// One lost answer is not a lost lease: the relay keeps its profile for
		// 60 s after the last keepalive it accepted. Ask again, with a new
		// sequence, while that margin lasts.
		if !errors.Is(err, errRadioTimeout) || attempt >= 3 || e.clock.Now().Sub(e.lastMaintain)+e.responseTimeout >= 50*time.Second {
			return fmt.Errorf("relay rendezvous lease not maintained: %w", err)
		}
		e.emit("rendezvous_keepalive_retry", map[string]any{"sequence": e.maintainSequence, "attempt": attempt})
	}
	if r.Radio == nil || !r.Radio.Known || r.Radio.Phase != 2 || r.Radio.Sequence != e.maintainSequence {
		return errors.New("relay rendezvous keepalive proof mismatch")
	}
	expected := e.normal
	if e.fast {
		expected = e.transfer
	}
	if !r.Radio.Armed || r.Radio.Service != e.savedNormal || r.Radio.Applied != expected {
		return errors.New("relay no longer uses the expected radio profile")
	}
	e.lastMaintain = e.clock.Now()
	return nil
}
func (e *nativeEngine) wait(d time.Duration) error {
	if e.rendezvousRelay == nil {
		e.clock.Sleep(d)
		return e.checkPause()
	}
	end := e.clock.Now().Add(d)
	for e.clock.Now().Before(end) {
		if err := e.checkPause(); err != nil {
			return err
		}
		if err := e.maintainRendezvous(); err != nil {
			return err
		}
		e.clock.Sleep(min(100*time.Millisecond, end.Sub(e.clock.Now())))
	}
	return nil
}
func (e *nativeEngine) startRendezvous() error {
	if e.targetNormal == nil || *e.targetNormal == e.normal {
		return nil
	}
	if e.relay == "" {
		return errors.New("different service profiles currently require a selected relay")
	}
	control := *e.targetNormal
	controlBudget := max(35*time.Second, 4*control.airtime(255)+5*time.Second)
	burst := 32 * (e.transfer.airtime(208) + 20*time.Millisecond)
	windowBudget := max(20*time.Second, 4*burst+4*e.transfer.airtime(255)+5*time.Second)
	if controlBudget+10*time.Second+2*control.airtime(100) >= 60*time.Second ||
		windowBudget+10*time.Second+2*e.transfer.airtime(100) >= 60*time.Second {
		return errors.New("radio transactions leave insufficient margin for the 60-second rendezvous lease")
	}
	// Prove the controller's own service before changing any remote radio.
	if err := e.bridge.command("RADIOSERVICE", profileLine("RADIOSERVICE", e.savedNormal), 4*time.Second, time.Second); err != nil {
		return fmt.Errorf("controller configured service profile not verified: %w", err)
	}
	if err := e.bridge.command("RADIOPROFILE", profileLine("RADIOPROFILE", e.savedNormal), 4*time.Second, time.Second); err != nil {
		return fmt.Errorf("controller service profile not verified: %w", err)
	}
	relay := &nativeEngine{bridge: e.bridge, clock: e.clock, key: e.relayKey, source: e.source, target: e.relay,
		normal: e.normal, transfer: e.transfer, normalTimeout: e.normalTimeout, responseTimeout: e.normalTimeout,
		counterFile: filepath.Join(filepath.Dir(e.counterFile), key(e.source+"\x00"+e.relay)+".counter"), pauseFile: e.pauseFile, event: e.event}
	initial, err := relay.radioStateRetry(3)
	if err != nil {
		return err
	}
	if initial.Service != e.savedNormal || initial.Applied != e.savedNormal || initial.Phase != 0 || initial.Armed || initial.RouteBusy {
		return errors.New("relay is not idle on its declared service profile")
	}
	if _, err = relay.hello(3); err != nil {
		return err
	}
	e.rendezvousRelay = relay
	e.rendezvousSuspended = false
	e.lastMaintain = e.clock.Now()
	e.rendezvousMayHaveMoved = true
	payload := append(encodeProfile(*e.targetNormal), identity(e.preparation.Recipient)...)
	if _, err = relay.request(opRendezvous, 0, payload, e.normalTimeout, 1); err != nil {
		return err
	}
	e.clock.Sleep(2200 * time.Millisecond)
	e.normal = *e.targetNormal
	if err = e.configureProfiles(); err != nil {
		return err
	}
	state, err := relay.radioState()
	if err != nil {
		return err
	}
	if state.Service != e.savedNormal || state.Applied != e.normal || state.Phase != 2 {
		return errors.New("relay rendezvous radio profile not established")
	}
	e.emit("rendezvous_established", map[string]any{"relay": e.relay, "service": state.Service, "applied": state.Applied})
	return nil
}
func profileLine(prefix string, p Profile) string {
	return fmt.Sprintf("%s %d %d %d %d %d", prefix, p.Frequency, p.Bandwidth, p.SF, p.CR, p.Power)
}
func (e *nativeEngine) verifyTargetService() error {
	state, err := e.radioState()
	if err != nil {
		return err
	}
	if state.Service != e.normal || state.Applied != e.normal {
		return errors.New("target service profile not verified")
	}
	e.emit("target_service_verified", map[string]any{"profile": state.Applied})
	return nil
}
func (e *nativeEngine) finishRendezvous() error {
	relay := e.rendezvousRelay
	if relay == nil {
		return nil
	}
	// Do not let the normal keepalive hook renew a lease while closing it.
	e.rendezvousRelay = nil
	relay.closing = true
	_, releaseErr := relay.request(opRelease, 0, nil, e.normalTimeout, 1)
	// A missing agreement is not proof that the relay stayed put. Silence lets
	// its independent 60-second outer lease restore its own service regardless.
	if releaseErr != nil {
		e.clock.Sleep(65 * time.Second)
	} else {
		e.clock.Sleep(2200 * time.Millisecond)
	}
	e.normal = e.savedNormal
	if err := e.configureProfiles(); err != nil {
		return errors.Join(releaseErr, err)
	}
	// The relay resumes its own station duties once back on its service: a
	// beacon or a repeated packet can hide one probe, so ask up to three times.
	state, err := relay.radioStateRetry(3)
	if err != nil {
		return errors.Join(releaseErr, err)
	}
	if state.Phase != 0 || state.Armed || state.RouteBusy || state.Service != e.savedNormal || state.Applied != e.savedNormal {
		return errors.New("relay service restoration not verified")
	}
	e.rendezvousMayHaveMoved = false
	e.emit("relay_service_restored", map[string]any{"relay": e.relay, "profile": state.Applied})
	return nil
}

// The relay resumes its own station duties on its service profile: a beacon or
// a repeated packet can hide one probe, so a state proof may be asked again.
func (e *nativeEngine) radioStateRetry(attempts int) (state *radioState, err error) {
	for attempt := 0; attempt < attempts; attempt++ {
		if state, err = e.radioState(); err == nil {
			return state, nil
		}
	}
	return nil, err
}

// After a lost link inside a rendezvous, silence is the only state both ends
// share: the transfer lease and the rendezvous lease both expire within 60 s,
// the relay returns to its own service and the target to its own. Establish
// the rendezvous again from there instead of renewing a lease that may be gone.
func (e *nativeEngine) recoverRendezvous() error {
	e.rendezvousSuspended = true
	if err := e.bridgeProfile(false); err != nil {
		return err
	}
	e.emit("waiting_for_profile_recovery", map[string]any{"seconds": 65, "rendezvous": "restart"})
	if err := e.wait(65 * time.Second); err != nil {
		return err
	}
	e.normal = e.savedNormal
	if err := e.configureProfiles(); err != nil {
		return err
	}
	if err := e.startRendezvous(); err != nil {
		return err
	}
	if _, err := e.hello(3); err != nil {
		return err
	}
	return e.enterFast()
}
