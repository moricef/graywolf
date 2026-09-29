package messages

import (
	"strings"
	"sync"
)

// ReplyAcks holds the APRS11 reply-ack state per correspondent
// (aprs.org/aprs11/replyacks.txt). When a message arrives in the
// "{MM}AA" form, its MM is remembered and piggybacked as AA on every
// later outgoing message to that station, at transmission time, so a
// retry carries the latest value. Stations that never sent the reply-ack
// form get no piggybacked ack. State is in memory only: after a restart
// the next incoming reply-ack message restores it.
type ReplyAcks struct {
	mu     sync.Mutex
	latest map[string]string
}

// NewReplyAcks returns an empty reply-ack state.
func NewReplyAcks() *ReplyAcks {
	return &ReplyAcks{latest: make(map[string]string)}
}

func replyAckKey(call string) string {
	return strings.ToUpper(strings.TrimSpace(call))
}

// Observe records the message number of a reply-ack capable message
// received from peer. A nil receiver is a no-op.
func (r *ReplyAcks) Observe(peer, msgID string) {
	if r == nil || peer == "" || msgID == "" {
		return
	}
	r.mu.Lock()
	r.latest[replyAckKey(peer)] = msgID
	r.mu.Unlock()
}

// Pending returns the ack to piggyback on the next message to peer, or
// "" when none is known. A nil receiver returns "".
func (r *ReplyAcks) Pending(peer string) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest[replyAckKey(peer)]
}

// wireMessageID renders the on-air message number of an outgoing
// message: "MM}AA", or "MM}" when no ack is pending. The trailing '}'
// advertises reply-ack support. An empty msgID stays empty.
func wireMessageID(msgID, pendingAck string) string {
	if msgID == "" {
		return ""
	}
	return msgID + "}" + pendingAck
}
