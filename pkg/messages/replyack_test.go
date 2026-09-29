package messages

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestWireMessageID(t *testing.T) {
	cases := []struct{ id, ack, want string }{
		{"", "AB", ""},
		{"01", "", "01}"},
		{"01", "AB", "01}AB"},
	}
	for _, c := range cases {
		if got := wireMessageID(c.id, c.ack); got != c.want {
			t.Errorf("wireMessageID(%q,%q)=%q want %q", c.id, c.ack, got, c.want)
		}
	}
}

func TestReplyAcksLatestPerPeer(t *testing.T) {
	r := NewReplyAcks()
	if got := r.Pending("W1ABC"); got != "" {
		t.Fatalf("empty state Pending=%q", got)
	}
	r.Observe("w1abc", "12")
	r.Observe("W1ABC", "13")
	if got := r.Pending("W1ABC "); got != "13" {
		t.Fatalf("Pending=%q want latest 13", got)
	}
	if got := r.Pending("K0XYZ"); got != "" {
		t.Fatalf("other peer Pending=%q", got)
	}
	var nilState *ReplyAcks
	nilState.Observe("W1ABC", "12")
	if got := nilState.Pending("W1ABC"); got != "" {
		t.Fatalf("nil state Pending=%q", got)
	}
}

func TestSenderWireIDPiggybacksPendingAck(t *testing.T) {
	acks := NewReplyAcks()
	s := &Sender{cfg: SenderConfig{ReplyAcks: acks}}
	row := &configstore.Message{PeerCall: "W1ABC", MsgID: "0A"}
	if got := s.wireID(row); got != "0A}" {
		t.Fatalf("no pending ack: wireID=%q want 0A}", got)
	}
	acks.Observe("W1ABC", "12")
	if got := s.wireID(row); got != "0A}12" {
		t.Fatalf("pending ack: wireID=%q want 0A}12", got)
	}
}
