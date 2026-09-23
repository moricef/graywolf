package app

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/rxttelemetry"
	"github.com/chrissnell/graywolf/pkg/stationcache"
)

// aprsJSONProduce preserves a protocol reception before the stream cursor can
// advance. Parsed envelopes feed passive APRS semantics (packet log, station
// cache and map) directly from their textual model. They never enter an AX.25
// output fanout merely because an address happens to be representable.
func (a *App) aprsJSONProduce(ctx context.Context, reception rxttelemetry.RawReception) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.plog == nil {
		return fmt.Errorf("APRS JSON packet log is unavailable")
	}

	entry := packetlog.Entry{
		Direction: packetlog.DirRX,
		Source:    "aprs-json",
		Display:   string(reception.RawTNC2),
		APRSJSON:  &reception,
	}
	if reception.Packet == nil {
		entry.Notes = "TNC2 envelope unavailable (" + reception.ParseStatus + ")"
		if len(reception.Warnings) > 0 {
			entry.Notes += ": " + strings.Join(reception.Warnings, "; ")
		}
		a.plog.Record(entry)
		return nil
	}

	pkt, err := aprs.ParseTNC2Packet(reception.Packet)
	if err != nil {
		entry.Notes = "APRS decode: " + err.Error()
		a.plog.Record(entry)
		return nil
	}
	pkt.Direction = aprs.DirectionRF
	entry.Type = string(pkt.Type)
	entry.Decoded = pkt
	if a.stationCache != nil {
		if entries := stationcache.ExtractEntry(pkt, "aprs-json", "RX", 0); len(entries) > 0 {
			a.stationCache.Update(entries)
		}
		if ev, ok := stationcache.BuildRxEvent(pkt); ok {
			a.stationCache.RecordRxEvent(ev)
		}
	}
	a.plog.Record(entry)
	return nil
}

// kissTncProduce is the RxIngress callback wired into kiss.Manager. It
// performs a non-blocking send of (rf, src) onto the shared rxFanout
// channel; on drop (consumer saturated) the app-level counter
// increments so the failure mode is observable rather than silent.
//
// Non-blocking is deliberate: a stuck consumer must drop off-air KISS
// frames before it back-pressures the KISS server's socket reader,
// which could in turn back-pressure the physical hardware TNC.
func (a *App) kissTncProduce(rf *pb.ReceivedFrame, src ingress.Source) {
	if rf == nil {
		return
	}
	select {
	case a.rxFanout <- rxFanoutItem{rf: rf, src: src}:
		if a.metrics != nil {
			a.metrics.ObserveKissTncRxDispatched(src.ID)
		}
	default:
		a.rxFanoutDropped.Add(1)
		if a.metrics != nil {
			a.metrics.RxFanoutDropped.WithLabelValues("kiss_tnc").Inc()
		}
	}
}

// audioLevelFromFrame projects a modem ReceivedFrame's mark/space tone
// amplitudes into a packetlog.AudioLevel. The Rust demodulator reports linear
// peak amplitudes where a full-scale tone is ~1.0. The level is expressed in
// dBFS (20·log10 of the linear amplitude, floored at -60) so it shares the
// real-time device meter's unit — a healthy −25 dBFS signal reads ≈ −25 in
// both places. The legacy linear ×100 Mark/Space ints are kept for backward
// compatibility. Returns nil when the frame carries no level (both zero), e.g.
// a modem build that didn't populate the fields, so the UI renders a dash
// rather than an empty meter.
func audioLevelFromFrame(rf *pb.ReceivedFrame) *packetlog.AudioLevel {
	mark, space := rf.AudioLevelMark, rf.AudioLevelSpace
	if mark <= 0 && space <= 0 {
		return nil
	}
	scale := func(v float32) int {
		if v <= 0 {
			return 0
		}
		return int(v*100 + 0.5)
	}
	clamp := func(v float32) float64 {
		if v <= 0 {
			return 0
		}
		return float64(v)
	}
	level := (clamp(mark) + clamp(space)) / 2
	return &packetlog.AudioLevel{
		Mark:      scale(mark),
		Space:     scale(space),
		MarkDBFS:  toDBFS(clamp(mark)),
		SpaceDBFS: toDBFS(clamp(space)),
		LevelDBFS: toDBFS(level),
	}
}

// toDBFS converts a linear amplitude (1.0 = full scale) to dBFS, floored at
// -60 to match the device meter's clamp. A non-positive amplitude (silence or
// the demod's -1.0 "unset" placeholder) maps to the -60 floor. The result is
// rounded to one decimal place.
func toDBFS(amp float64) float64 {
	const floor = -60.0
	if amp <= 0 {
		return floor
	}
	db := 20 * math.Log10(amp)
	if db < floor {
		db = floor
	}
	db = math.Round(db*10) / 10
	if db == 0 {
		// Normalize -0.0 (e.g. amp just under 1.0) so JSON emits "0", not "-0".
		db = 0
	}
	return db
}

// dispatchRxFrame runs the fanout consumer's per-frame work: KISS broadcast,
// digi handling, AGW monitoring, APRS decode + submit, station cache update,
// and packet-log recording. This fanout accepts physical modem and KISS-TNC
// inputs. LoRa APRS JSON receptions use the separate receive-only textual
// path in aprsJSONProduce and cannot enter this AX.25 output path.
func (a *App) dispatchRxFrame(ctx context.Context, item rxFanoutItem, aprsSubmit *aprsSubmitter) {
	rf := item.rf
	src := item.src
	var (
		logSource string
		skipID    uint32
		skip      bool
	)
	switch src.Kind {
	case ingress.KindModem:
		logSource = "modem"
	case ingress.KindKissTnc:
		logSource = "kiss-tnc"
		skipID = src.ID
		skip = true
	default:
		if a.logger != nil {
			a.logger.Warn("rx fanout: unknown ingress kind; dropping frame",
				"kind", src.Kind, "channel", rf.Channel)
		}
		return
	}

	a.kissMgr.BroadcastFromChannel(rf.Channel, rf.Data, skipID, skip)

	// Per-packet audio level comes from the soundcard demodulator only.
	// Hardware KISS-TNC frames arrive already demodulated, so they carry no
	// meaningful mark/space level — leave it nil for those.
	var alevel *packetlog.AudioLevel
	if src.Kind == ingress.KindModem {
		alevel = audioLevelFromFrame(rf)
	}

	// Raw KISS clients (e.g. Xastir) do their own decoding and want every
	// frame, including ones graywolf itself fails to decode — so this must
	// run before the decode early-return below, not after it.
	if srv := a.currentAgwServer(); srv != nil {
		srv.BroadcastRawKISS(rf.Channel, rf.Data)
	}

	f, err := ax25.Decode(rf.Data)
	if err != nil {
		a.plog.Record(packetlog.Entry{
			Channel:    rf.Channel,
			Direction:  packetlog.DirRX,
			Source:     logSource,
			Raw:        rf.Data,
			AudioLevel: alevel,
		})
		return
	}

	e := packetlog.Entry{
		Channel:    rf.Channel,
		Direction:  packetlog.DirRX,
		Source:     logSource,
		Raw:        rf.Data,
		Display:    f.String(),
		AudioLevel: alevel,
	}

	// Per-frame debug log for KISS-TNC ingest — Phase 5 of the KISS
	// modem/TNC plan. Modem-RX is not logged here because existing TX
	// and packet-log paths already cover those events.
	if src.Kind == ingress.KindKissTnc && a.logger != nil {
		a.logger.Debug("kiss tnc ingress",
			"interface_id", src.ID,
			"channel", rf.Channel,
			"frame_len", len(rf.Data),
			"source_callsign", f.Source.String(),
		)
	}

	if f.IsUI() {
		if srv := a.currentAgwServer(); srv != nil {
			srv.BroadcastMonitoredUI(rf.Channel, f)
		}
		a.digi.Handle(ctx, rf.Channel, f, src)
		if pkt, err := aprs.Parse(f); err == nil && pkt != nil {
			pkt.Channel = int(rf.Channel)
			pkt.Direction = aprs.DirectionRF
			e.Type = string(pkt.Type)
			e.Decoded = pkt
			// Actions classifier first: if this is a "@@"-prefixed
			// message addressed to our trigger surface, divert it
			// from the messages router into the Actions runner. The
			// classifier returns false for everything else (non-message,
			// not addressed to us, no @@ prefix), so the inbox keeps
			// receiving normal traffic. Station cache still updates
			// either way so action senders remain visible in the
			// heard-station table.
			consumed := false
			if a.actions != nil {
				consumed = a.actions.Classifier().Classify(ctx, pkt)
			}
			if !consumed {
				aprsSubmit.submit(pkt)
			}
			if entries := stationcache.ExtractEntry(pkt, logSource, "RX", rf.Channel); len(entries) > 0 {
				a.stationCache.Update(entries)
			}
			// Heatmap: count this physical RF reception exactly once, here at
			// the sole off-air ingest edge — not in the station cache write
			// path, which also runs for the iGate RF->IS re-gate and the
			// startup roster reload (neither a fresh reception).
			if ev, ok := stationcache.BuildRxEvent(pkt); ok {
				a.stationCache.RecordRxEvent(ev)
			}
		}
	} else if a.ax25Mgr != nil {
		// Connected-mode dispatch: any non-UI frame goes to the LAPB
		// manager, which decodes it with the owning session's negotiated
		// modulus (mod-8 vs mod-128) and drops it if no session matches
		// (channel, local, peer). Decoding here with a fixed modulus would
		// corrupt mod-128 sessions' N(S)/N(R) — see graywolf #456.
		a.ax25Mgr.DispatchRaw(rf.Channel, rf.Data)
	}
	a.plog.Record(e)
}
