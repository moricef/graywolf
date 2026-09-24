// Package tnc2link handles the line-oriented TNC2 transport used by LoRa
// iGates. It does not convert packets to AX.25 or authorize RF transmission.
package tnc2link

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/chrissnell/graywolf/pkg/tnc2"
)

const maxRecordBytes = 65536

var ErrDisconnected = errors.New("tnc2 link: disconnected")
var ErrQueueFull = errors.New("tnc2 link: transmit queue full")

// Link is one connected, bidirectional TCP or serial TNC2 stream. A complete
// wire record is one TNC2 packet followed by CRLF. Receiver diagnostic lines
// are ignored, never interpreted as APRS packets.
type Link struct {
	conn             io.ReadWriteCloser
	writeMu          sync.Mutex
	maxTransmitBytes int
}

func NewLink(conn io.ReadWriteCloser) *Link { return &Link{conn: conn} }

// NewLimitedLink sets the peer's input-record limit. CA2RXU currently uses a
// 255-byte TNC2 input buffer; other TNCs may advertise a different limit.
func NewLimitedLink(conn io.ReadWriteCloser, maxTransmitBytes int) *Link {
	return &Link{conn: conn, maxTransmitBytes: maxTransmitBytes}
}

// ReadPackets reads until the peer disconnects or ctx is cancelled. The
// callback receives the losslessly parsed textual envelope, including binary
// Mic-E information bytes. It must return promptly to avoid backpressure.
func (l *Link) ReadPackets(ctx context.Context, onPacket func(*tnc2.TNC2Packet)) error {
	if l == nil || l.conn == nil {
		return ErrDisconnected
	}
	if onPacket == nil {
		return errors.New("tnc2 link: nil packet callback")
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.conn.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	scanner := bufio.NewScanner(l.conn)
	scanner.Buffer(make([]byte, 4096), maxRecordBytes)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) > 0 && raw[len(raw)-1] == '\r' {
			raw = raw[:len(raw)-1]
		}
		// Serial output also carries console/debug and decoded RXT lines.
		// Only an actual TNC2 envelope is passed into Graywolf.
		if !validHeader(raw) {
			continue
		}
		packet, err := tnc2.Parse(raw)
		if err == nil {
			onPacket(packet)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return scanner.Err()
}

// SendPacket writes the exact TNC2 bytes plus CRLF. CR/LF within the record
// cannot be represented by this transport and are rejected instead of being
// split into two on-air transmissions. No AX.25 adapter is involved.
func (l *Link) SendPacket(ctx context.Context, raw []byte) error {
	if l == nil || l.conn == nil {
		return ErrDisconnected
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidatePacket(raw, l.maxTransmitBytes); err != nil {
		return err
	}
	line := make([]byte, 0, len(raw)+2)
	line = append(line, raw...)
	line = append(line, '\r', '\n')
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	for len(line) > 0 {
		n, err := l.conn.Write(line)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		line = line[n:]
	}
	return nil
}

// ValidatePacket checks the newline framing and the configured peer limit
// before a packet is accepted into an asynchronous transmit queue.
func ValidatePacket(raw []byte, maxTransmitBytes int) error {
	if bytes.IndexAny(raw, "\r\n") >= 0 {
		return errors.New("tnc2 link: packet contains a line separator")
	}
	if maxTransmitBytes > 0 && len(raw) > maxTransmitBytes {
		return fmt.Errorf("tnc2 link: packet length %d exceeds peer input limit %d", len(raw), maxTransmitBytes)
	}
	if !validHeader(raw) {
		return errors.New("tnc2 link: invalid packet header")
	}
	if _, err := tnc2.Parse(raw); err != nil {
		return fmt.Errorf("tnc2 link: invalid packet: %w", err)
	}
	return nil
}

func (l *Link) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	return l.conn.Close()
}

// validHeader excludes status/debug lines before the permissive lossless
// TNC2 parser sees them. Extended suffixes remain opaque and unrestricted.
func validHeader(raw []byte) bool {
	colon := bytes.IndexByte(raw, ':')
	if colon < 0 {
		return false
	}
	header := raw[:colon]
	gt := bytes.IndexByte(header, '>')
	if gt <= 0 || gt == len(header)-1 {
		return false
	}
	for _, c := range header {
		if c <= ' ' || c >= 0x7f {
			return false
		}
	}
	return true
}
