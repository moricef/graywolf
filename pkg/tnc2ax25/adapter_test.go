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
