package rxttelemetry

import (
	"github.com/chrissnell/graywolf/pkg/aprs"
	"strings"
	"time"
)

// ReceptionLinks projects measured radio TX->RX pairs, never the original
// packet source through a relay. It is shared by the overlay and receiver DX.
func ReceptionLinks(r RawReception, at time.Time) []Link {
	var links []Link
	add := func(h Hop) {
		if !h.HasData || h.From == "" || h.To == "" || h.From == h.To || strings.TrimSpace(h.From) != h.From || strings.TrimSpace(h.To) != h.To || strings.EqualFold(h.From, "UNKNOWN") || strings.EqualFold(h.To, "UNKNOWN") {
			return
		}
		links = append(links, Link{Hop: h, ObservedAt: at, Packet: string(r.RawTNC2)})
	}
	if r.RXT != nil {
		for _, h := range r.RXT.Hops {
			hop := Hop{From: h.TX, To: h.RX, HasData: h.HasData}
			if h.RSSI != nil {
				hop.RSSI = *h.RSSI
			}
			if h.SNR != nil {
				hop.SNR = *h.SNR
			}
			if h.FO != nil {
				hop.FO = *h.FO
			}
			if h.TTH != nil {
				hop.TTH = *h.TTH
			}
			add(hop)
		}
	}
	if r.Local != nil && r.Packet != nil {
		from := r.Packet.Source.Text
		for _, address := range r.Packet.Path {
			if address.Repeated && !aprs.IsGenericPathAlias(address.Text) {
				from = strings.TrimSuffix(address.Text, "*")
			}
		}
		add(Hop{From: from, To: r.Receiver, HasData: true, RSSI: r.Local.RSSI, SNR: r.Local.SNR, FO: int(r.Local.FO)})
	}
	return links
}
