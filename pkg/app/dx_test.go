package app

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
	"github.com/chrissnell/graywolf/pkg/rfdx"
)

func TestDXReportsAndQueriesUseStationRoutesWithoutBeaconRows(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "F4MLV-2"}); err != nil {
		t.Fatal(err)
	}
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{Latitude: 43, Longitude: 1, Timestamp: time.Now()})
	tracker := rfdx.New(cache)
	tracker.Record(&aprs.DecodedAPRSPacket{Source: "FAR", Channel: 1, Direction: aprs.DirectionRF, Position: &aprs.Position{Latitude: 45, Longitude: 1}})
	rf := testtx.NewRecorder()
	is := &fakeISLineSink{}
	scheduler, err := beacon.New(beacon.Options{Sink: rf, ISSink: is})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, rfDX: tracker, beaconSched: scheduler, dxReplies: make(chan dxReply, 4)}
	c := rfdx.DefaultConfig()
	c.Channel = 1
	a.configureDX(c)
	if err = a.sendDXReport(ctx, "status"); err != nil {
		t.Fatal(err)
	}
	if len(is.lines) != 1 || !strings.Contains(is.lines[0], ":>Graywolf DX=FAR(") || rf.Len() != 0 {
		t.Fatalf("status routing: %+v", is.lines)
	}
	if err = a.sendDXReport(ctx, "capabilities"); err != nil {
		t.Fatal(err)
	}
	if len(is.lines) != 2 || !strings.Contains(is.lines[1], ":<IGATE,MSG_CNT=") {
		t.Fatalf("capabilities: %+v", is.lines)
	}
	rows, _ := store.ListBeacons(ctx)
	if len(rows) != 0 {
		t.Fatal("reports created beacons")
	}
	p := &aprs.DecodedAPRSPacket{Source: "QUERY", Direction: aprs.DirectionRF, Channel: 1, Message: &aprs.Message{Addressee: "F4MLV-2", Text: "?DX"}}
	a.handleDXQuery(p)
	select {
	case reply := <-a.dxReplies:
		if reply.config.SendPath != "rf" || reply.config.Channel != 1 || !strings.HasPrefix(reply.info, ":QUERY    :DX: FAR ") || strings.Contains(reply.info, "{") {
			t.Fatalf("query reply: %+v", reply)
		}
	default:
		t.Fatal("query not handled")
	}
	a.handleDXQuery(p)
	if len(a.dxReplies) != 0 {
		t.Fatal("query retry bypassed throttle")
	}
	p.Source = "OTHER"
	p.Direction = aprs.DirectionIS
	a.handleDXQuery(p)
	select {
	case reply := <-a.dxReplies:
		if reply.config.SendPath != "is_only" {
			t.Fatal("IS query leaked to RF")
		}
	default:
		t.Fatal("IS query not handled")
	}
	p.Source = "BAD"
	p.Message.MessageID = "01"
	a.handleDXQuery(p)
	if len(a.dxReplies) != 0 {
		t.Fatal("numbered query responded")
	}
}

func TestDXComponentFlushesRecordOnImmediateStop(t *testing.T) {
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tracker := rfdx.New(nil)
	tracker.RestoreRecords([]rfdx.Result{{Channel: 1, Callsign: "RECORD", DistanceKM: 139, ReceivedAt: time.Now().UTC()}})
	a := &App{store: store, rfDX: tracker, logger: slog.Default(), dxConfig: rfdx.DefaultConfig(), dxWake: make(chan struct{}, 1), dxReplies: make(chan dxReply, 1)}
	component := a.dxComponent()
	if err = component.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = component.stop(ctx); err != nil {
		t.Fatal(err)
	}
	records, err := store.LoadDXRecords(context.Background())
	if err != nil || len(records) != 1 || records[0].Callsign != "RECORD" {
		t.Fatalf("record lost during fast restart: %+v %v", records, err)
	}
}
