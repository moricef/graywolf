package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
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
	cfg := a.currentTNC2Config()
	if cfg.TXTransport == "" || a.gov == nil {
		return errTNC2TXDisabled
	}
	if err := tnc2link.ValidatePacket(raw, int(cfg.MaxTXBytes)); err != nil {
		return err
	}
	packet, err := tnc2.Parse(raw)
	if err != nil {
		return err
	}
	if packet.Source.Text != cfg.TXSource {
		return fmt.Errorf("TNC2 source %q is not the configured transmit source", packet.Source.Text)
	}
	a.tnc2Mu.RLock()
	client := a.tnc2ByTransport[cfg.TXTransport]
	connected := client != nil && client.Connected()
	a.tnc2Mu.RUnlock()
	if !connected {
		return tnc2link.ErrDisconnected
	}
	return a.gov.SubmitTNC2(ctx, cfg.TXChannel, raw, source)
}

type tnc2MessageRF struct{ app *App }

func (t tnc2MessageRF) Enabled(channel uint32) bool {
	cfg := t.app.currentTNC2Config()
	return cfg.TXTransport != "" && channel == cfg.TXChannel
}

func (t tnc2MessageRF) Submit(ctx context.Context, channel uint32, raw []byte, source txgovernor.SubmitSource) error {
	if !t.Enabled(channel) {
		return errTNC2TXDisabled
	}
	return t.app.submitAuthorizedTNC2(ctx, raw, source)
}

func (a *App) sendTNC2Text(channel uint32, raw []byte, _ txgovernor.SubmitSource) error {
	cfg := a.currentTNC2Config()
	if cfg.TXTransport == "" || channel != cfg.TXChannel {
		return errTNC2TXDisabled
	}
	a.tnc2Mu.RLock()
	client := a.tnc2ByTransport[cfg.TXTransport]
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
	cfg := a.currentTNC2Config()
	if source == "tnc2-"+cfg.TXTransport && cfg.TXTransport != "" {
		decoded.Channel = int(cfg.TXChannel)
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
		Channel: a.currentTNC2Config().TXChannel, Direction: packetlog.DirTX,
		Source: source, Display: string(raw), TNC2: packet,
	}
	if decoded, err := aprs.ParseTNC2Packet(packet); err == nil {
		decoded.Channel = int(entry.Channel)
		decoded.Direction = aprs.DirectionRF
		entry.Type = string(decoded.Type)
		entry.Decoded = decoded
		if a.stationCache != nil {
			if entries := stationcache.ExtractEntry(decoded, "tnc2", "TX", entry.Channel); len(entries) > 0 {
				a.stationCache.Update(entries)
			}
		}
	}
	a.plog.Record(entry)
}

func (a *App) tnc2Component() namedComponent {
	return namedComponent{
		name: "TNC2 transceivers",
		start: func(ctx context.Context) error {
			a.tnc2Mu.Lock()
			a.tnc2Ctx = ctx
			a.tnc2Mu.Unlock()
			a.applyTNC2Config(a.currentTNC2Config())
			return nil
		},
		stop: func(shutdownCtx context.Context) error {
			a.tnc2Mu.Lock()
			if a.tnc2Cancel != nil {
				a.tnc2Cancel()
			}
			a.tnc2Ctx = nil
			a.tnc2Cancel = nil
			a.tnc2Mu.Unlock()
			return waitGroup(shutdownCtx, &a.tnc2WG, "TNC2 transceivers")
		},
	}
}

func (a *App) flagTNC2Config() configstore.TNC2Config {
	return configstore.TNC2Config{TCPAddress: a.cfg.TNC2TCP, SerialDevice: a.cfg.TNC2SerialDevice,
		SerialBaud: uint32(a.cfg.TNC2SerialBaud), TXTransport: a.cfg.TNC2TXTransport,
		TXSource: a.cfg.TNC2TXSource, TXChannel: uint32(a.cfg.TNC2TXChannel), MaxTXBytes: uint32(a.cfg.TNC2MaxTXBytes)}
}

func (a *App) currentTNC2Config() configstore.TNC2Config {
	a.tnc2Mu.RLock()
	defer a.tnc2Mu.RUnlock()
	if a.tnc2CfgLoaded {
		return a.tnc2Cfg
	}
	return a.flagTNC2Config()
}

func (a *App) tnc2Settings() (configstore.TNC2Config, bool, bool) {
	a.tnc2Mu.RLock()
	defer a.tnc2Mu.RUnlock()
	cfg := a.flagTNC2Config()
	if a.tnc2CfgLoaded {
		cfg = a.tnc2Cfg
	}
	tcp, serial := a.tnc2ByTransport[tnc2link.TransportTCP], a.tnc2ByTransport[tnc2link.TransportSerial]
	return cfg, tcp != nil && tcp.Connected(), serial != nil && serial.Connected()
}

func (a *App) updateTNC2Config(ctx context.Context, cfg configstore.TNC2Config) error {
	cfg.TCPAddress = strings.TrimSpace(cfg.TCPAddress)
	cfg.SerialDevice = strings.TrimSpace(cfg.SerialDevice)
	cfg.TXSource = strings.TrimSpace(cfg.TXSource)
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := a.store.UpsertTNC2Config(ctx, cfg); err != nil {
		return err
	}
	a.applyTNC2Config(cfg)
	if a.beaconReload != nil {
		select {
		case a.beaconReload <- struct{}{}:
		default:
		}
	}
	return nil
}

func (a *App) applyTNC2Config(cfg configstore.TNC2Config) {
	a.tnc2Mu.Lock()
	defer a.tnc2Mu.Unlock()
	if a.tnc2Cancel != nil {
		a.tnc2Cancel()
	}
	a.tnc2Cfg, a.tnc2CfgLoaded = cfg, true
	a.tnc2ByTransport = make(map[string]*tnc2link.Client)
	if a.tnc2Ctx == nil {
		return
	}
	ctx, cancel := context.WithCancel(a.tnc2Ctx)
	a.tnc2Cancel = cancel
	start := func(transport string, clientCfg tnc2link.ClientConfig) {
		source := "tnc2-" + transport
		clientCfg.OnPacket = func(packet *tnc2.TNC2Packet) { a.tnc2Produce(source, packet) }
		if cfg.TXTransport == transport {
			clientCfg.OnSent = func(raw []byte) { a.tnc2Sent(source, raw) }
			clientCfg.MaxTransmitBytes = int(cfg.MaxTXBytes)
		}
		clientCfg.OnSendError = func(err error) {
			if a.logger != nil {
				a.logger.Error("TNC2 packet transmission failed", "transport", transport, "err", err)
			}
		}
		client := tnc2link.NewClient(clientCfg)
		a.tnc2ByTransport[transport] = client
		a.tnc2WG.Add(1)
		go func() {
			defer a.tnc2WG.Done()
			if err := client.Run(ctx); err != nil && a.logger != nil {
				a.logger.Error("TNC2 transceiver stopped", "source", source, "err", err)
			}
		}()
	}
	if cfg.TCPAddress != "" {
		start(tnc2link.TransportTCP, tnc2link.ClientConfig{Transport: tnc2link.TransportTCP, Address: cfg.TCPAddress})
	}
	if cfg.SerialDevice != "" {
		start(tnc2link.TransportSerial, tnc2link.ClientConfig{Transport: tnc2link.TransportSerial, Device: cfg.SerialDevice, BaudRate: cfg.SerialBaud})
	}
}
