package tnc2

import (
	"bytes"
	"testing"
)

func TestParsePreservesExtendedAddressesAndRepeatedMarker(t *testing.T) {
	raw := []byte("F4JJE-16>NN7LE-GS,NN7LE-S*,WIDE1-1:payload")
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Raw, raw) || !bytes.Equal(p.Information, []byte("payload")) {
		t.Fatalf("raw or information changed: %#v", p)
	}
	if p.Source.Text != "F4JJE-16" || p.Source.Call != "F4JJE" || p.Source.Suffix != "16" {
		t.Fatalf("source = %#v", p.Source)
	}
	if p.Destination.Text != "NN7LE-GS" || p.Destination.Suffix != "GS" {
		t.Fatalf("destination = %#v", p.Destination)
	}
	if got := p.Path[0]; got.Text != "NN7LE-S*" || got.Call != "NN7LE" || got.Suffix != "S" || !got.Repeated {
		t.Fatalf("repeated path = %#v", got)
	}
}

func TestParsePreservesBinaryInformation(t *testing.T) {
	raw := append([]byte("N0CALL>APRS:"), 0x60, 0x9c, 0x01, 0xff)
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Information, []byte{0x60, 0x9c, 0x01, 0xff}) {
		t.Fatalf("information = %x", p.Information)
	}
}

func TestAX25SSIDDoesNotBoundOpaqueSuffixStorage(t *testing.T) {
	tests := []struct {
		suffix string
		ssid   uint8
		ok     bool
	}{
		{"", 0, true},
		{"15", 15, true},
		{"16", 0, false},
		{"GS", 0, false},
		{"184467440737095516160000000000", 0, false},
	}
	for _, tt := range tests {
		a := PacketAddress{Suffix: tt.suffix}
		ssid, ok := a.AX25SSID()
		if ssid != tt.ssid || ok != tt.ok {
			t.Errorf("suffix %q: got (%d, %v), want (%d, %v)", tt.suffix, ssid, ok, tt.ssid, tt.ok)
		}
	}
}
