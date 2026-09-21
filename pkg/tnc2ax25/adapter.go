// Package tnc2ax25 contains the one-way, checked adapter from the extended
// textual TNC2 model to classic AX.25 UI frames.
package tnc2ax25

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/tnc2"
)

// ToFrame converts p only when every address is representable in classic
// AX.25. A failed conversion never mutates the lossless TNC2 packet.
func ToFrame(p *tnc2.TNC2Packet) (*ax25.Frame, error) {
	if p == nil {
		return nil, fmt.Errorf("tnc2ax25: nil packet")
	}
	source, err := address(p.Source)
	if err != nil {
		return nil, fmt.Errorf("tnc2ax25: source %q: %w", p.Source.Text, err)
	}
	destination, err := address(p.Destination)
	if err != nil {
		return nil, fmt.Errorf("tnc2ax25: destination %q: %w", p.Destination.Text, err)
	}
	path := make([]ax25.Address, 0, len(p.Path))
	for _, textual := range p.Path {
		converted, err := address(textual)
		if err != nil {
			return nil, fmt.Errorf("tnc2ax25: path %q: %w", textual.Text, err)
		}
		path = append(path, converted)
	}
	return ax25.NewUIFrame(source, destination, path, p.Information)
}

func address(a tnc2.PacketAddress) (ax25.Address, error) {
	ssid, ok := a.AX25SSID()
	if !ok {
		return ax25.Address{}, fmt.Errorf("suffix %q is not an AX.25 SSID", a.Suffix)
	}
	// Validate only the call here. Extended parsing itself deliberately does
	// not depend on ax25.ParseAddress.
	parsed, err := ax25.ParseAddress(a.Call)
	if err != nil {
		return ax25.Address{}, err
	}
	parsed.SSID = ssid
	parsed.Repeated = a.Repeated
	return parsed, nil
}
