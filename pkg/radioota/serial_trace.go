package radioota

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Console errors can echo authentication commands or provisioning values.
// Never retain long encoded tokens, even in unrecognized diagnostic lines.
var serialEncodedToken = regexp.MustCompile(`[A-Za-z0-9_+/=-]{16,}`)

func serialTraceText(text string) string {
	text = serialEncodedToken.ReplaceAllString(text, "[redacted]")
	if len(text) > 1024 {
		text = text[:1024] + " [truncated]"
	}
	return text
}

func serialTraceFields(line string) map[string]any {
	fields := map[string]any{"bytes": len(line)}
	words := strings.Fields(line)
	if len(words) > 0 && (words[0] == "TX" || words[0] == "TXRAW" || words[0] == "RX") {
		// RF frames include firmware data and HMACs. Log only their envelope
		// metadata, not the hex payload, including for malformed input.
		fields["line"] = words[0] + " [frame content omitted]"
		if len(words) != 2 {
			fields["malformed"] = true
			return fields
		}
		frame, err := hex.DecodeString(words[1])
		if err != nil {
			fields["malformed"] = true
			return fields
		}
		fields["frame_bytes"] = len(frame)
		if len(frame) >= 35 && string(frame[:4]) == "\x3c\xff\x7e\x02" {
			fields["op"] = int(frame[4])
			fields["offset"] = binary.BigEndian.Uint32(frame[13:17])
		}
		return fields
	}
	fields["line"] = serialTraceText(strings.TrimSpace(line))
	return fields
}

func (b *bridge) traceEvent(event string, fields map[string]any) {
	if b.trace != nil {
		b.trace(event, fields)
	}
}

func (b *bridge) traceError(event string, err error) {
	b.traceEvent(event, map[string]any{"error": serialTraceText(fmt.Sprint(err)), "partial_bytes": len(b.buffer)})
}
