package rxttelemetry

import (
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"testing"
	"time"
)

func TestReceptionLinksUsesActualTransmitter(t *testing.T) {
	packet, err := tnc2.Parse([]byte("ORIGIN>APLRG1,F4MLV-10*,WIDE2-1:!4400.00N/00100.00E#test"))
	if err != nil {
		t.Fatal(err)
	}
	r := RawReception{CRCValid: true, Receiver: "F1ZDB-13", Packet: packet, Local: &LocalMetrics{RSSI: -90, SNR: 4}, RXT: &RXTTelemetry{Hops: []RXTHop{{TX: "ORIGIN", RX: "F4MLV-10", HasData: true}, {TX: "GHOST", RX: "F4MLV-10", HasData: false}}}}
	links := ReceptionLinks(r, time.Now())
	if len(links) != 2 || links[0].From != "ORIGIN" || links[0].To != "F4MLV-10" || links[1].From != "F4MLV-10" || links[1].To != "F1ZDB-13" {
		t.Fatalf("incorrect radio attribution: %+v", links)
	}
}
