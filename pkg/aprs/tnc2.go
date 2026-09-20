package aprs

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// FormatTNC2 renders a TNC-2 wire line: source>dest[,path...]:info.
//
// Used by both originated traffic (locally-built APRS-IS submissions
// such as outbound messages or beacons) and gated traffic (RF packets
// re-emitted to APRS-IS). Each caller owns the path and info bytes;
// this helper only owns the structural glue:
//
//   - the `>` between source and dest
//   - the `,` between path elements
//   - the `:` between the path field and the info field
//
// The path-terminator `:` is the load-bearing detail. APRS-IS consumers
// (aprs.fi and the like) reject lines missing it as "Unsupported packet
// format" — a class of bug that bit graywolf in May 2026 when the
// originating-side encoder was emitting only a single colon. Both
// encoders going through this helper makes that recurrence impossible.
//
// Empty path elements are silently dropped. The info bytes are written
// verbatim — the caller is responsible for any APRS data-type
// identifier that has to lead them (`:` for messages, `!` for position,
// etc.).
func FormatTNC2(source, dest string, path []string, info []byte) string {
	var b strings.Builder
	b.Grow(len(source) + 1 + len(dest) + 16 + len(info))
	b.WriteString(source)
	b.WriteByte('>')
	b.WriteString(dest)
	for _, p := range path {
		if p == "" {
			continue
		}
		b.WriteByte(',')
		b.WriteString(p)
	}
	b.WriteByte(':')
	b.Write(info)
	return b.String()
}

// ParseTNC2 parses a TNC-2 monitor record into an AX.25 UI frame. The input
// is bytes rather than a string so binary APRS information (notably Mic-E)
// survives unchanged. Repeated markers on path addresses are preserved.
func ParseTNC2(raw []byte) (*ax25.Frame, error) {
	if len(raw) == 0 {
		return nil, errors.New("aprs: empty TNC2 record")
	}
	colon := bytes.IndexByte(raw, ':')
	if colon < 0 {
		return nil, errors.New("aprs: TNC2 record missing ':' info separator")
	}
	header, info := raw[:colon], raw[colon+1:]
	if len(info) == 0 {
		return nil, errors.New("aprs: TNC2 record has empty info field")
	}
	gt := bytes.IndexByte(header, '>')
	if gt <= 0 {
		return nil, errors.New("aprs: TNC2 record missing '>' source/dest separator")
	}

	source, err := ax25.ParseAddress(string(header[:gt]))
	if err != nil {
		return nil, fmt.Errorf("aprs: parse TNC2 source: %w", err)
	}
	parts := bytes.Split(header[gt+1:], []byte{','})
	if len(parts) == 0 || len(parts[0]) == 0 {
		return nil, errors.New("aprs: TNC2 record missing destination")
	}
	dest, err := ax25.ParseAddress(string(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("aprs: parse TNC2 destination: %w", err)
	}
	path := make([]ax25.Address, 0, len(parts)-1)
	for _, part := range parts[1:] {
		if len(part) == 0 {
			continue
		}
		address, err := ax25.ParseAddress(string(part))
		if err != nil {
			return nil, fmt.Errorf("aprs: parse TNC2 path %q: %w", part, err)
		}
		path = append(path, address)
	}
	return ax25.NewUIFrame(source, dest, path, info)
}
