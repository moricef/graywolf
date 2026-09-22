package configstore

import (
	"context"
	"encoding/json"
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
	if err := store.UpdateRXTResume(ctx, got.Endpoint, "boot-1:9", "boot-1", true); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetRXTConfig(ctx)
	if err != nil || got.LastEventID != "boot-1:9" || got.LastBootID != "boot-1" || !got.ResumeSupported {
		t.Fatalf("stored resume state = %+v, err=%v", got, err)
	}
	if err := store.UpsertRXTConfig(ctx, RXTConfig{Endpoint: got.Endpoint}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetRXTConfig(ctx)
	if got.LastEventID != "boot-1:9" {
		t.Fatalf("same endpoint reset cursor: %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":1,"endpoint":"http://igate/rxt.json"}` {
		t.Fatalf("public JSON exposes resume state: %s", encoded)
	}
	if err := store.UpsertRXTConfig(ctx, RXTConfig{Endpoint: "http://igate/api/v1/aprs/stream"}); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetRXTConfig(ctx)
	if got.LastEventID != "" || got.LastBootID != "" || got.ResumeSupported {
		t.Fatalf("changed endpoint retained cursor: %+v", got)
	}
}

func TestReplaceRXTConfigsPreservesUnchangedResumeState(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "graywolf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	configs, err := store.ReplaceRXTConfigs(ctx, []string{
		"http://local/api/v1/aprs/stream", "http://firmin/rxt.json",
	})
	if err != nil || len(configs) != 2 {
		t.Fatalf("initial configs=%+v err=%v", configs, err)
	}
	if err := store.UpdateRXTResume(ctx, "http://local/api/v1/aprs/stream", "boot:8", "boot", true); err != nil {
		t.Fatal(err)
	}
	configs, err = store.ReplaceRXTConfigs(ctx, []string{
		"http://local/api/v1/aprs/stream", "http://third/api/v1/aprs/stream",
	})
	if err != nil || len(configs) != 2 {
		t.Fatalf("replaced configs=%+v err=%v", configs, err)
	}
	byEndpoint := make(map[string]RXTConfig)
	for _, config := range configs {
		byEndpoint[config.Endpoint] = config
	}
	if byEndpoint["http://local/api/v1/aprs/stream"].LastEventID != "boot:8" {
		t.Fatalf("unchanged source lost cursor: %+v", byEndpoint)
	}
	if _, ok := byEndpoint["http://firmin/rxt.json"]; ok {
		t.Fatalf("removed source retained: %+v", byEndpoint)
	}
}
