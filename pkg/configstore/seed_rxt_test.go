package configstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRXTConfigRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "graywolf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	got, err := store.GetRXTConfig(ctx)
	if err != nil || got.Endpoint != "" {
		t.Fatalf("fresh config = %+v, err=%v", got, err)
	}
	if err := store.UpsertRXTConfig(ctx, RXTConfig{Endpoint: " http://igate/rxt.json "}); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetRXTConfig(ctx)
	if err != nil || got.Endpoint != "http://igate/rxt.json" {
		t.Fatalf("stored config = %+v, err=%v", got, err)
	}
}
