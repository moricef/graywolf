// Package tnc2 models and parses TNC2-compatible packet envelopes without
// imposing AX.25 address limits on their textual identities.
package tnc2

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// PacketAddress is a lossless textual address from a TNC2 envelope. Text is
// authoritative and, for path elements, includes the received trailing '*'.
// Suffix is opaque; it is never parsed into a general-purpose integer.
type PacketAddress struct {
	Text     string `json:"text"`
	Call     string `json:"call"`
	Suffix   string `json:"suffix,omitempty"`
	Repeated bool   `json:"repeated,omitempty"`
}

// AX25SSID reports whether the suffix can be represented in the four-bit
// classic AX.25 SSID field. It deliberately rejects by inspecting digits and
// range directly, so an arbitrarily long numeric suffix is never truncated or
// forced through a bounded integer type.
func (a PacketAddress) AX25SSID() (uint8, bool) {
	if a.Suffix == "" {
		return 0, true
	}
	var value uint8
	for i := 0; i < len(a.Suffix); i++ {
		c := a.Suffix[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		digit := c - '0'
		if value > 1 || (value == 1 && digit > 5) {
			return 0, false
		}
		value = value*10 + digit
	}
	return value, true
}

// TNC2Packet is the lossless parsed envelope. Raw and Information may contain
// arbitrary bytes; callers must not round-trip them through strings.
type TNC2Packet struct {
	Raw         []byte          `json:"raw"`
	Source      PacketAddress   `json:"source"`
	Destination PacketAddress   `json:"destination"`
	Path        []PacketAddress `json:"path"`
	Information []byte          `json:"information"`
}

// Parse decodes the TNC2 envelope only. It does not validate APRS semantics
// and does not depend on the AX.25 address model.
func Parse(raw []byte) (*TNC2Packet, error) {
	if len(raw) == 0 {
		return nil, errors.New("tnc2: empty record")
	}
	colon := bytes.IndexByte(raw, ':')
	if colon < 0 {
		return nil, errors.New("tnc2: missing ':' information separator")
	}
	header := raw[:colon]
	gt := bytes.IndexByte(header, '>')
	if gt <= 0 {
		return nil, errors.New("tnc2: missing '>' source/destination separator")
	}

	source, err := parseAddress(string(header[:gt]), false)
	if err != nil {
		return nil, fmt.Errorf("tnc2: source: %w", err)
	}
	parts := bytes.Split(header[gt+1:], []byte{','})
	if len(parts) == 0 || len(parts[0]) == 0 {
		return nil, errors.New("tnc2: missing destination")
	}
	destination, err := parseAddress(string(parts[0]), false)
	if err != nil {
		return nil, fmt.Errorf("tnc2: destination: %w", err)
	}
	path := make([]PacketAddress, 0, len(parts)-1)
	for _, part := range parts[1:] {
		if len(part) == 0 {
			return nil, errors.New("tnc2: empty path element")
		}
		address, err := parseAddress(string(part), true)
		if err != nil {
			return nil, fmt.Errorf("tnc2: path %q: %w", part, err)
		}
		path = append(path, address)
	}

	return &TNC2Packet{
		Raw:         bytes.Clone(raw),
		Source:      source,
		Destination: destination,
		Path:        path,
		Information: bytes.Clone(raw[colon+1:]),
	}, nil
}

func parseAddress(text string, allowRepeated bool) (PacketAddress, error) {
	if text == "" {
		return PacketAddress{}, errors.New("empty address")
	}
	a := PacketAddress{Text: text}
	identity := text
	if allowRepeated && strings.HasSuffix(identity, "*") {
		a.Repeated = true
		identity = strings.TrimSuffix(identity, "*")
		if identity == "" {
			return PacketAddress{}, errors.New("empty repeated address")
		}
	}
	a.Call = identity
	if dash := strings.IndexByte(identity, '-'); dash >= 0 {
		if dash == 0 || dash == len(identity)-1 {
			return PacketAddress{}, errors.New("empty call or suffix")
		}
		a.Call = identity[:dash]
		a.Suffix = identity[dash+1:]
	}
	return a, nil
}
