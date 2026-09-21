package aprs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"github.com/chrissnell/graywolf/pkg/tnc2ax25"
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
	packet, err := tnc2.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("aprs: parse TNC2: %w", err)
	}
	if len(packet.Information) == 0 {
		return nil, errors.New("aprs: TNC2 record has empty info field")
	}
	frame, err := tnc2ax25.ToFrame(packet)
	if err != nil {
		return nil, fmt.Errorf("aprs: convert TNC2 to AX.25: %w", err)
	}
	return frame, nil
}
