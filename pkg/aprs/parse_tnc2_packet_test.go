package aprs

import (
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
	dest := EncodeMicEDest(35.5, 1, false, true, 0)
	info := []byte{
		'`',
		byte(72 + 28), byte(30 + 28), byte(0 + 28),
		byte(0 + 28), byte(0 + 28), byte(0 + 28),
		'>', '/',
	}
	raw := append([]byte("F4JJE-16>"+dest+":"), info...)
	p, err := tnc2.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseTNC2Packet(p)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Source != "F4JJE-16" || decoded.Type != PacketMicE || decoded.MicE == nil {
		t.Fatalf("Mic-E packet = %+v", decoded)
	}
}
