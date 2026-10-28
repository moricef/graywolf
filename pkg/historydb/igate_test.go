package historydb

import (
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"path/filepath"
	"testing"
	"time"
)

func TestIGateHistoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pos := stationcache.CacheEntry{Key: "stn:F4GCF-14", Callsign: "F4GCF-14", HasPos: true, Lat: 43, Lon: 0.5, Symbol: [2]byte{'/', 'Z'}, Comment: "position comment", Timestamp: now}
	report := stationcache.CacheEntry{Key: pos.Key, Callsign: pos.Callsign, Timestamp: now, IGate: &stationcache.IGateReport{ReceivedAt: now, Entries: map[string]string{"IGATE": "", "RF_CNT": "29"}}}
	if err = db.WriteEntries([]stationcache.CacheEntry{pos, report, pos}); err != nil {
		t.Fatal(err)
	}
	stale := report
	stale.IGate = &stationcache.IGateReport{ReceivedAt: now.Add(-time.Minute), Entries: map[string]string{"IGATE": "", "RF_CNT": "1"}}
	if err = db.WriteEntries([]stationcache.CacheEntry{stale}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, err := db.LoadRecent(time.Hour, 200)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded[pos.Key]
	if got == nil || got.IGate == nil || got.IGate.Entries["RF_CNT"] != "29" || !got.IGate.ReceivedAt.Equal(now) || got.Symbol != pos.Symbol || got.Comment != pos.Comment || len(got.Positions) != 1 {
		t.Fatalf("bad restored station: %+v", got)
	}
}
