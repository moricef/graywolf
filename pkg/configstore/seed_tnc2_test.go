package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTNC2ConfigPersistsAndValidates(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "graywolf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, exists, err := store.GetTNC2Config(ctx); err != nil || exists {
		t.Fatalf("fresh config exists=%v err=%v", exists, err)
	}
	cfg := TNC2Config{TCPAddress: "igate:8001", SerialDevice: "/dev/ttyUSB0", SerialBaud: 115200,
		TXTransport: "serial", TXSource: "F4MLV-GS", TXChannel: 1, MaxTXBytes: 255}
	if err := store.UpsertTNC2Config(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	got, exists, err := store.GetTNC2Config(ctx)
	if err != nil || !exists || got.TXTransport != "serial" || got.SerialDevice != cfg.SerialDevice {
		t.Fatalf("stored=%+v exists=%v err=%v", got, exists, err)
	}
	for _, bad := range []TNC2Config{
		{TXTransport: "serial", TXSource: "F4MLV-GS", TXChannel: 1, MaxTXBytes: 255},
		{TCPAddress: "igate:8001", TXTransport: "tcp", TXSource: "F4MLV-GS", TXChannel: 1},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("accepted invalid config: %+v", bad)
		}
	}
	got.TXTransport = ""
	if err := store.UpsertTNC2Config(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, exists, err = store.GetTNC2Config(ctx)
	if err != nil || !exists || got.TXTransport != "" {
		t.Fatalf("disabled TX not persisted: %+v, %v", got, err)
	}
}
