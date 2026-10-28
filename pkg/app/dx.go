package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/rfdx"
)

type dxReply struct {
	config rfdx.Config
	info   string
}

func (a *App) configureDX(c rfdx.Config) {
	a.dxMu.Lock()
	a.dxConfig = c
	a.rfDX.Configure(c)
	if a.receiverDX != nil {
		a.receiverDX.Configure(c)
	}
	a.dxMu.Unlock()
	select {
	case a.dxWake <- struct{}{}:
	default:
	}
}

func (a *App) currentDXConfig() rfdx.Config {
	a.dxMu.RLock()
	defer a.dxMu.RUnlock()
	return a.dxConfig
}

func (a *App) clearDXRecords(ctx context.Context) error {
	a.dxMu.Lock()
	defer a.dxMu.Unlock()
	if err := a.store.ClearDXRecords(ctx); err != nil {
		return err
	}
	a.rfDX.ClearRecords()
	return nil
}

func (a *App) sendDXInfo(ctx context.Context, c rfdx.Config, info string) error {
	if c.Channel == 0 && c.SendPath != beacon.SendPathISOnly {
		return fmt.Errorf("select an RF receive channel in DX / Status")
	}
	call, err := a.store.ResolveStationCallsign(ctx)
	if err != nil {
		return err
	}
	row := configstore.Beacon{Type: "custom", Channel: c.Channel, Callsign: call, Destination: "APGRWO", Path: c.Path, CustomInfo: info, SendPath: c.SendPath}
	bc, err := beaconConfigFromStoreWithMode(row, nil, call, (tnc2MessageRF{app: a}).Enabled(c.Channel))
	if err != nil {
		return err
	}
	return a.beaconSched.SendReport(ctx, bc)
}

func (a *App) sendDXReport(ctx context.Context, kind string) error {
	c := a.currentDXConfig()
	info, err := a.rfDX.Report(c.Channel, kind, c.StatusText, c.IncludeDX)
	if err != nil {
		return err
	}
	return a.sendDXInfo(ctx, c, info)
}

func (a *App) dxComponent() namedComponent {
	return namedComponent{name: "DX and station reports", start: func(parent context.Context) error {
		ctx, cancel := context.WithCancel(parent)
		a.dxCancel = cancel
		a.dxWG.Add(1)
		go func() {
			defer a.dxWG.Done()
			defer func() {
				finalCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
				defer done()
				a.dxMu.Lock()
				defer a.dxMu.Unlock()
				if err := a.store.SaveDXRecords(finalCtx, a.rfDX.Snapshot().MaxEver); err != nil {
					a.logger.Warn("save final DX records", "err", err)
				}
			}()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			var schedule rfdx.Schedule
			reset := func() {
				c := a.currentDXConfig()
				now := time.Now()
				schedule = rfdx.NewSchedule(c, now)
			}
			reset()
			lastRecords := ""
			persist := func() {
				a.dxMu.Lock()
				defer a.dxMu.Unlock()
				records := a.rfDX.Snapshot().MaxEver
				encoded, _ := json.Marshal(records)
				if string(encoded) != lastRecords {
					if err := a.store.SaveDXRecords(ctx, records); err != nil {
						a.logger.Warn("save DX record", "err", err)
					} else {
						lastRecords = string(encoded)
					}
				}
			}
			for {
				select {
				case <-ctx.Done():
					return
				case <-a.dxWake:
					reset()
				case reply := <-a.dxReplies:
					requestCtx, done := context.WithTimeout(ctx, 2*time.Second)
					if err := a.sendDXInfo(requestCtx, reply.config, reply.info); err != nil {
						a.logger.Debug("DX query reply failed", "err", err)
					}
					done()
				case now := <-tick.C:
					persist()
					c := a.currentDXConfig()
					send := func(kind string) {
						if err := a.sendDXReport(ctx, kind); err != nil {
							a.logger.Warn("station report failed", "kind", kind, "err", err)
						}
					}
					statusDue := false
					for _, kind := range schedule.Due(c, now) {
						if kind == "status" {
							statusDue = true
						}
						send(kind)
					}
					if notifications := a.rfDX.TakeNotifications(); len(notifications) > 0 && !statusDue && c.StatusEnabled && c.IncludeDX {
						send("status")
					}
				}
			}
		}()
		return nil
	}, stop: func(ctx context.Context) error {
		if a.dxCancel != nil {
			a.dxCancel()
		}
		return waitGroup(ctx, &a.dxWG, "DX reports")
	}}
}

// Directed queries receive a one-shot unnumbered reply on the ingress transport.
// They do not create reliable-message retries or APRS acknowledgements.
func (a *App) handleDXQuery(p *aprs.DecodedAPRSPacket) {
	if a.rfDX == nil || a.store == nil || a.beaconSched == nil || p == nil || p.Message == nil || p.Message.MessageID != "" || len(p.Source) > 9 {
		return
	}
	query := strings.ToUpper(strings.TrimSpace(p.Message.Text))
	if query != "?DX" && query != "?IGATE?" && query != "?IGATE" {
		return
	}
	call, err := a.store.ResolveStationCallsign(context.Background())
	if err != nil || !strings.EqualFold(strings.TrimSpace(p.Message.Addressee), call) {
		return
	}
	a.dxQueryMu.Lock()
	if a.dxQueryLast == nil {
		a.dxQueryLast = make(map[string]time.Time)
	}
	now := time.Now()
	if now.Sub(a.dxQueryLast[p.Source]) < 30*time.Second {
		a.dxQueryMu.Unlock()
		return
	}
	for k, v := range a.dxQueryLast {
		if now.Sub(v) > time.Minute {
			delete(a.dxQueryLast, k)
		}
	}
	a.dxQueryLast[p.Source] = now
	a.dxQueryMu.Unlock()
	c := a.currentDXConfig()
	if p.Direction == aprs.DirectionRF {
		if p.Channel <= 0 {
			return
		}
		c.Channel = uint32(p.Channel)
		c.SendPath = beacon.SendPathRF
		c.Path = ""
	} else if p.Direction == aprs.DirectionIS {
		c.SendPath = beacon.SendPathISOnly
		if c.Channel == 0 {
			var farthest float64
			for _, dx := range a.rfDX.Snapshot().Channels {
				if dx.DistanceKM > farthest {
					c.Channel = dx.Channel
					farthest = dx.DistanceKM
				}
			}
		}
	} else {
		return
	}
	var info string
	if query == "?DX" {
		text := "DX: no qualifying direct RF reception"
		for _, r := range a.rfDX.Snapshot().Channels {
			if r.Channel == c.Channel {
				text = fmt.Sprintf("DX: %s %.0fkm@%03ddeg %sZ", r.Callsign, r.DistanceKM, int(r.BearingDeg+0.5)%360, r.ReceivedAt.UTC().Format("15:04"))
				break
			}
		}
		info = fmt.Sprintf(":%-9s:%s", p.Source, text)
	} else {
		info, _ = a.rfDX.Report(c.Channel, "capabilities", "", c.IncludeDX)
	}
	// The bounded component-owned queue prevents stalled TX from blocking RX.
	select {
	case a.dxReplies <- dxReply{config: c, info: info}:
	default:
	}
}
