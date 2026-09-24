package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"github.com/chrissnell/graywolf/pkg/tnc2link"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

var errTNC2TXDisabled = errors.New("TNC2 transmission is disabled")

// submitTNC2TX is the sole direct-text transmit entry point. A connected
// receiver alone never grants transmit permission, even for AX.25-compatible
// text. The exact configured textual source is checked before queueing.
func (a *App) submitTNC2TX(ctx context.Context, raw []byte) error {
	return a.submitAuthorizedTNC2(ctx, raw, txgovernor.SubmitSource{
		Kind: "tnc2", Priority: txgovernor.PriorityClient,
	})
}

func (a *App) submitAuthorizedTNC2(ctx context.Context, raw []byte, source txgovernor.SubmitSource) error {
	if a.cfg.TNC2TXTransport == "" || a.gov == nil {
		return errTNC2TXDisabled
	}
	if err := tnc2link.ValidatePacket(raw, int(a.cfg.TNC2MaxTXBytes)); err != nil {
		return err
	}
	packet, err := tnc2.Parse(raw)
	if err != nil {
		return err
	}
	if packet.Source.Text != a.cfg.TNC2TXSource {
		return fmt.Errorf("TNC2 source %q is not the configured transmit source", packet.Source.Text)
	}
	a.tnc2Mu.RLock()
	client := a.tnc2ByTransport[a.cfg.TNC2TXTransport]
	connected := client != nil && client.Connected()
	a.tnc2Mu.RUnlock()
	if !connected {
		return tnc2link.ErrDisconnected
	}
	return a.gov.SubmitTNC2(ctx, uint32(a.cfg.TNC2TXChannel), raw, source)
}

type tnc2MessageRF struct{ app *App }

func (t tnc2MessageRF) Enabled(channel uint32) bool {
	return t.app.cfg.TNC2TXTransport != "" && channel == uint32(t.app.cfg.TNC2TXChannel)
}

func (t tnc2MessageRF) Submit(ctx context.Context, channel uint32, raw []byte, source txgovernor.SubmitSource) error {
	if !t.Enabled(channel) {
		return errTNC2TXDisabled
	}
	return t.app.submitAuthorizedTNC2(ctx, raw, source)
}

func (a *App) sendTNC2Text(channel uint32, raw []byte, _ txgovernor.SubmitSource) error {
	if a.cfg.TNC2TXTransport == "" || channel != uint32(a.cfg.TNC2TXChannel) {
		return errTNC2TXDisabled
	}
	a.tnc2Mu.RLock()
	client := a.tnc2ByTransport[a.cfg.TNC2TXTransport]
	a.tnc2Mu.RUnlock()
	if client == nil {
		return tnc2link.ErrDisconnected
	}
	return client.EnqueuePacket(raw)
}

// tnc2Produce is the direct serial/TCP receive path. It deliberately uses
// the textual APRS model rather than the AX.25 fanout: receiving a packet
// never authorizes retransmission to RF, KISS, APRS-IS or a digipeater.
func (a *App) tnc2Produce(source string, packet *tnc2.TNC2Packet) {
	if packet == nil || a.plog == nil {
		return
	}
	entry := packetlog.Entry{
		Direction: packetlog.DirRX,
		Source:    source,
		Display:   string(packet.Raw),
		TNC2:      packet,
	}
	decoded, err := aprs.ParseTNC2Packet(packet)
	if err != nil {
		entry.Notes = "APRS decode: " + err.Error()
		a.plog.Record(entry)
		return
	}
	decoded.Direction = aprs.DirectionRF
	decoded.TextualIngress = true
	if source == "tnc2-serial" && decoded.Message != nil && (decoded.Message.IsAck || decoded.Message.IsRej) && a.logger != nil {
		a.logger.Info("TNC2 serial acknowledgement received",
			"from", packet.Source.Text,
			"to", decoded.Message.Addressee,
			"id", decoded.Message.MessageID,
			"rejected", decoded.Message.IsRej)
	}
	if source == "tnc2-"+a.cfg.TNC2TXTransport && a.cfg.TNC2TXTransport != "" {
		decoded.Channel = int(a.cfg.TNC2TXChannel)
	}
	entry.Type = string(decoded.Type)
	entry.Decoded = decoded
	if a.stationCache != nil {
		if entries := stationcache.ExtractEntry(decoded, source, "RX", 0); len(entries) > 0 {
			a.stationCache.Update(entries)
		}
		if ev, ok := stationcache.BuildRxEvent(decoded); ok {
			a.stationCache.RecordRxEvent(ev)
		}
	}
	a.plog.Record(entry)
	if a.msgSvc != nil {
		_ = a.msgSvc.Router().SendPacket(context.Background(), decoded)
	}
}

func (a *App) tnc2Sent(source string, raw []byte) {
	if a.plog == nil {
		return
	}
	packet, err := tnc2.Parse(raw)
	if err != nil {
		return // validated before it entered the TX queue
	}
	entry := packetlog.Entry{
		Channel: uint32(a.cfg.TNC2TXChannel), Direction: packetlog.DirTX,
		Source: source, Display: string(raw), TNC2: packet,
	}
	if decoded, err := aprs.ParseTNC2Packet(packet); err == nil {
		entry.Type = string(decoded.Type)
		entry.Decoded = decoded
	}
	a.plog.Record(entry)
}

func (a *App) tnc2Component() namedComponent {
	return namedComponent{
		name: "TNC2 transceivers",
		start: func(ctx context.Context) error {
			start := func(transport, source string, cfg tnc2link.ClientConfig) {
				cfg.OnPacket = func(packet *tnc2.TNC2Packet) { a.tnc2Produce(source, packet) }
				if a.cfg.TNC2TXTransport == transport {
					cfg.OnSent = func(raw []byte) { a.tnc2Sent(source, raw) }
				}
				cfg.OnSendError = func(err error) {
					if a.logger != nil {
						a.logger.Error("TNC2 packet transmission failed", "transport", transport, "err", err)
					}
				}
				if a.cfg.TNC2TXTransport == transport {
					cfg.MaxTransmitBytes = int(a.cfg.TNC2MaxTXBytes)
				}
				client := tnc2link.NewClient(cfg)
				a.tnc2Mu.Lock()
				a.tnc2Clients = append(a.tnc2Clients, client)
				if a.tnc2ByTransport == nil {
					a.tnc2ByTransport = make(map[string]*tnc2link.Client)
				}
				a.tnc2ByTransport[transport] = client
				a.tnc2Mu.Unlock()
				a.tnc2WG.Add(1)
				go func() {
					defer a.tnc2WG.Done()
					if err := client.Run(ctx); err != nil && a.logger != nil {
						a.logger.Error("TNC2 transceiver stopped", "source", source, "err", err)
					}
				}()
			}
			if a.cfg.TNC2TCP != "" {
				start(tnc2link.TransportTCP, "tnc2-tcp", tnc2link.ClientConfig{
					Transport: tnc2link.TransportTCP,
					Address:   a.cfg.TNC2TCP,
				})
			}
			if a.cfg.TNC2SerialDevice != "" {
				start(tnc2link.TransportSerial, "tnc2-serial", tnc2link.ClientConfig{
					Transport: tnc2link.TransportSerial,
					Device:    a.cfg.TNC2SerialDevice,
					BaudRate:  uint32(a.cfg.TNC2SerialBaud),
				})
			}
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			return waitGroup(shutdownCtx, &a.tnc2WG, "TNC2 transceivers")
		},
	}
}
