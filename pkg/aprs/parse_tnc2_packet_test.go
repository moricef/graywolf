package aprs

import (
	"bytes"
	"testing"

	"github.com/chrissnell/graywolf/pkg/tnc2"
)

func TestParseTNC2PacketDecodesAPRSWithExtendedTransportIdentity(t *testing.T) {
	p, err := tnc2.Parse([]byte("NN7LE-GS>APRS,NN7LE-S*:>extended station"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseTNC2Packet(p)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Source != "NN7LE-GS" || decoded.Dest != "APRS" || len(decoded.Path) != 1 || decoded.Path[0] != "NN7LE-S*" {
		t.Fatalf("transport identity changed: %+v", decoded)
	}
	if decoded.Type != PacketStatus || decoded.Status != "extended station" {
		t.Fatalf("APRS status = type %q status %q", decoded.Type, decoded.Status)
	}
	if decoded.DedupKey() == "" {
		t.Fatal("transport-independent packet has no dedup key")
	}
}

func TestParseTNC2PacketRetainsNonAPRSApplicationData(t *testing.T) {
	p, err := tnc2.Parse([]byte("N7UV-GS>TEXT:keyboard-to-keyboard"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseTNC2Packet(p)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Source != "N7UV-GS" || decoded.Type != PacketUnknown || decoded.Comment != "keyboard-to-keyboard" {
		t.Fatalf("non-APRS data changed: %+v", decoded)
	}
}

func TestParseTNC2PacketMicEUsesTextualDestination(t *testing.T) {
	tests := []struct {
		name string
		dti  byte
	}{
		{name: "old", dti: '\''},
		{name: "current", dti: '`'},
		{name: "current_rev0_beta", dti: 0x1c},
		{name: "old_rev0_beta", dti: 0x1d},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := []byte{
				tc.dti,
				'd', ':', 0x1c, // longitude 72°30.00'W
				'(', '<', '>', // speed 123 kt, course 234°
				'>', '/', // symbol code and table
			}
			raw := append([]byte("F4JJE-16>35SP0P:"), info...)
			p, err := tnc2.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(p.Raw, raw) || !bytes.Equal(p.Information, info) {
				t.Fatalf("Mic-E bytes changed: raw=%x info=%x", p.Raw, p.Information)
			}

			decoded, err := ParseTNC2Packet(p)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Source != "F4JJE-16" || decoded.Type != PacketMicE || decoded.MicE == nil || decoded.Position == nil {
				t.Fatalf("Mic-E packet = %+v", decoded)
			}
			assertFloatNear(t, "latitude", decoded.Position.Latitude, 35.5, 0.0001)
			assertFloatNear(t, "longitude", decoded.Position.Longitude, -72.5, 0.0001)
			if decoded.Position.Speed != 123 || !decoded.Position.HasCourse || decoded.Position.Course != 234 {
				t.Fatalf("motion = speed %.0f course %d has_course=%v", decoded.Position.Speed, decoded.Position.Course, decoded.Position.HasCourse)
			}
			if decoded.Position.Symbol.Table != '/' || decoded.Position.Symbol.Code != '>' {
				t.Fatalf("symbol = %q%q, want '/''>'", decoded.Position.Symbol.Table, decoded.Position.Symbol.Code)
			}
			if decoded.MicE.MessageCode != 1 || decoded.MicE.MessageText != "Priority" {
				t.Fatalf("message = code %d text %q", decoded.MicE.MessageCode, decoded.MicE.MessageText)
			}
		})
	}
}

func assertFloatNear(t *testing.T, field string, got, want, tolerance float64) {
	t.Helper()
	if got < want-tolerance || got > want+tolerance {
		t.Fatalf("%s = %.6f, want %.6f ± %.6f", field, got, want, tolerance)
	}
}
