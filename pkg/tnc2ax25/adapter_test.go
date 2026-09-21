package tnc2ax25

import (
	"bytes"
	"testing"

	"github.com/chrissnell/graywolf/pkg/tnc2"
)

func TestToFrameAcceptsClassicAddresses(t *testing.T) {
	p, err := tnc2.Parse([]byte("N0CALL-7>APRS,WIDE1-1*:>status"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ToFrame(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Source.String() != "N0CALL-7" || f.Path[0].String() != "WIDE1-1*" || !bytes.Equal(f.Info, []byte(">status")) {
		t.Fatalf("unexpected frame: %#v", f)
	}
}

func TestToFrameRejectsExtendedSuffixWithoutMutatingPacket(t *testing.T) {
	raw := []byte("F4JJE-16>APRS:>status")
	p, err := tnc2.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ToFrame(p); err == nil {
		t.Fatal("expected AX.25 conversion to fail")
	}
	if !bytes.Equal(p.Raw, raw) || p.Source.Text != "F4JJE-16" || p.Source.Suffix != "16" {
		t.Fatalf("packet was mutated: %#v", p)
	}
}

func TestToFrameRejectsHugeNumericSuffix(t *testing.T) {
	p, err := tnc2.Parse([]byte("N0CALL-184467440737095516160000000000>APRS:data"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ToFrame(p); err == nil {
		t.Fatal("expected AX.25 conversion to fail")
	}
}

func TestToFrameRequiresCanonicalRoundTripIdentity(t *testing.T) {
	tests := []struct {
		address string
		ok      bool
	}{
		{"N0CALL", true},
		{"N0CALL-1", true},
		{"N0CALL-15", true},
		{"N0CALL-0", false},
		{"N0CALL-00", false},
		{"N0CALL-01", false},
		{"N0CALL-00015", false},
		{"n0call-1", false},
		{"F4JJE-16", false},
		{"NN7LE-GS", false},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			p, err := tnc2.Parse([]byte(tt.address + ">APRS:>status"))
			if err != nil {
				t.Fatal(err)
			}
			frame, err := ToFrame(p)
			if tt.ok {
				if err != nil {
					t.Fatalf("canonical address rejected: %v", err)
				}
				if got := frame.Source.String(); got != tt.address {
					t.Fatalf("round trip = %q, want %q", got, tt.address)
				}
				return
			}
			if err == nil {
				t.Fatalf("non-canonical address converted as %q", frame.Source.String())
			}
		})
	}
}
