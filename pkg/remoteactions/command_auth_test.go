package remoteactions

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const publishedRemoteCommandSecret = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func newCommandCredentialStore(t *testing.T) *CommandCredentialStore {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Exec(`CREATE TABLE remote_command_credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		target_call TEXT NOT NULL UNIQUE,
		secret_b64url TEXT NOT NULL,
		key_id TEXT NOT NULL DEFAULT 'A',
		last_counter INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_used_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("schema: %v", err)
	}
	return NewCommandCredentialStore(db)
}

func TestBuildRemoteCommandEnvelopePublishedVector(t *testing.T) {
	got, err := BuildRemoteCommandEnvelope(
		publishedRemoteCommandSecret, "F4MLV-2", "F4MLV-10", 71, "TX=OFF",
	)
	if err != nil {
		t.Fatalf("BuildRemoteCommandEnvelope: %v", err)
	}
	const want = "!RC1:A:1Z:TX=OFF:OLkaJxyKIXBM9SrF"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildRemoteCommandEnvelopeBindsEveryField(t *testing.T) {
	base, err := BuildRemoteCommandEnvelope(
		publishedRemoteCommandSecret, "F4MLV-2", "F4MLV-10", 71, "TX=OFF",
	)
	if err != nil {
		t.Fatal(err)
	}
	variants := []struct {
		controller string
		target     string
		counter    uint64
		command    string
	}{
		{"F4MLV-3", "F4MLV-10", 71, "TX=OFF"},
		{"F4MLV-2", "F4MLV-11", 71, "TX=OFF"},
		{"F4MLV-2", "F4MLV-10", 72, "TX=OFF"},
		{"F4MLV-2", "F4MLV-10", 71, "TX=ON"},
	}
	for _, tc := range variants {
		got, err := BuildRemoteCommandEnvelope(
			publishedRemoteCommandSecret, tc.controller, tc.target, tc.counter, tc.command,
		)
		if err != nil {
			t.Fatal(err)
		}
		if got == base {
			t.Fatalf("variant produced baseline envelope: %+v", tc)
		}
	}
}

func TestNormalizeRemoteCommandSecret(t *testing.T) {
	if got, err := NormalizeRemoteCommandSecret(publishedRemoteCommandSecret); err != nil || got != publishedRemoteCommandSecret {
		t.Fatalf("valid secret: got %q err=%v", got, err)
	}
	for _, bad := range []string{"", "abc=", strings.Repeat("A", 42), strings.Repeat("!", 43)} {
		if _, err := NormalizeRemoteCommandSecret(bad); err == nil {
			t.Fatalf("accepted invalid secret %q", bad)
		}
	}
}

func TestReserveEnvelopePersistsCounterBeforeReturn(t *testing.T) {
	store := newCommandCredentialStore(t)
	if err := store.Create(context.Background(), &RemoteCommandCredential{
		Name: "F4MLV-15 control", TargetCall: "F4MLV-15", SecretBase64URL: publishedRemoteCommandSecret,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, counter, err := store.ReserveEnvelope(context.Background(), "F4MLV-15", "F4MLV-2", "TX=OFF")
	if err != nil {
		t.Fatalf("ReserveEnvelope: %v", err)
	}
	if counter != 1 || !strings.HasPrefix(first, "!RC1:A:1:TX=OFF:") {
		t.Fatalf("first envelope=%q counter=%d", first, counter)
	}
	row, err := store.GetByTarget(context.Background(), "F4MLV-15")
	if err != nil {
		t.Fatal(err)
	}
	if row.LastCounter != 1 {
		t.Fatalf("persisted counter=%d, want 1", row.LastCounter)
	}
	second, counter, err := store.ReserveEnvelope(context.Background(), "F4MLV-15", "F4MLV-2", "COMMIT")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if counter != 2 || !strings.HasPrefix(second, "!RC1:A:2:COMMIT:") || second == first {
		t.Fatalf("second envelope=%q counter=%d", second, counter)
	}
}

func TestReserveEnvelopeConcurrentCountersAreUnique(t *testing.T) {
	store := newCommandCredentialStore(t)
	if err := store.Create(context.Background(), &RemoteCommandCredential{
		Name: "F4MLV-15 control", TargetCall: "F4MLV-15", SecretBase64URL: publishedRemoteCommandSecret,
	}); err != nil {
		t.Fatal(err)
	}

	const count = 8
	values := make(chan uint64, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, counter, err := store.ReserveEnvelope(context.Background(), "F4MLV-15", "F4MLV-2", "TX=ON")
			if err != nil {
				errs <- err
				return
			}
			values <- counter
		}()
	}
	wg.Wait()
	close(values)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
	}
	seen := map[uint64]bool{}
	for value := range values {
		if seen[value] {
			t.Fatalf("duplicate counter %d", value)
		}
		seen[value] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d counters, want %d", len(seen), count)
	}
}
