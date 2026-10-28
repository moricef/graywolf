package app

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/tnc2"
)

func TestNativeTNC2DirectDXKeepsReceiveChannel(t *testing.T) {
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{Latitude: 43, Longitude: 1, Timestamp: time.Now()})
	a := &App{rfDX: rfdx.New(cache), plog: packetlog.New(packetlog.Config{Capacity: 100})}
	a.tnc2CfgLoaded = true
	a.tnc2Cfg = configstore.TNC2Config{TXTransport: "tcp", TXChannel: 2}
	packet, err := tnc2.Parse([]byte("NN7LE-GS>APRS,WIDE1-1:!4400.00N/00100.00E-test"))
	if err != nil {
		t.Fatal(err)
	}
	a.tnc2Produce("tnc2-tcp", packet)
	s := a.rfDX.Snapshot()
	if len(s.Channels) != 1 || s.Channels[0].Channel != 2 || s.Channels[0].Callsign != "NN7LE-GS" {
		t.Fatalf("native DX: %+v", s)
	}
	packet, _ = tnc2.Parse([]byte("FAR>APRS,DIGI*:!5000.00N/00100.00E-test"))
	a.tnc2Produce("tnc2-tcp", packet)
	if a.rfDX.Snapshot().Channels[0].Callsign != "NN7LE-GS" {
		t.Fatal("relayed source replaced direct DX")
	}
}
