package remoteactions

import "testing"

// The LoRa iGate firmware (REMOTE_AUTH::keyFingerprint) must show the same
// value for the same key. Vector computed independently with Python hashlib.
func TestKeyFingerprint(t *testing.T) {
	if got := KeyFingerprint("Xq3v-9Kd_2mLp0aB7cE4fG6hJ8kN1oQ5rS2tU3vW4xY"); got != "187cbf" {
		t.Fatalf("fingerprint = %q, want 187cbf", got)
	}
	for _, bad := range []string{"", "Xq3v-9Kd_2mLp0aB7cE4fG6hJ8kN1oQ5rS2tU3vW", "not a key"} {
		if got := KeyFingerprint(bad); got != "" {
			t.Fatalf("fingerprint(%q) = %q, want empty", bad, got)
		}
	}
}
