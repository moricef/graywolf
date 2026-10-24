// Package tncprofile initializes command-mode TNCs before handing their
// serial stream to the KISS transport. Native KISS devices remain untouched.
package tncprofile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	Native  = ""
	Kenwood = "kenwood"
	TMD700  = "kenwood-tm-d700"
	TMD710  = "kenwood-tm-d710"
)

func Valid(profile string) bool {
	return profile == Native || profile == Kenwood || profile == TMD700 || profile == TMD710
}

// Initialize owns the stream until initialization completes. A context
// timeout closes the port to interrupt blocking reads/writes. The returned
// wrapper preserves bytes already buffered after the command prompt.
func Initialize(ctx context.Context, port io.ReadWriteCloser, profile string) (io.ReadWriteCloser, error) {
	if profile == TMD700 || profile == TMD710 {
		return initialize(ctx, port, profile, 5*time.Second)
	}
	return initialize(ctx, port, profile, 1500*time.Millisecond)
}

func initialize(parent context.Context, port io.ReadWriteCloser, profile string, settle time.Duration) (io.ReadWriteCloser, error) {
	if profile == Native {
		return port, nil
	}
	if !Valid(profile) {
		return nil, fmt.Errorf("unknown TNC initialization profile %q", profile)
	}
	if profile == TMD700 || profile == TMD710 {
		return initializeModel(parent, port, profile, settle)
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stopClose()
	reader := bufio.NewReader(port)
	write := func(data string) error {
		n, err := io.WriteString(port, data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return err
	}
	prompt := func() error {
		var tail string
		for i := 0; i < 8192; i++ {
			b, err := reader.ReadByte()
			if err != nil {
				return err
			}
			tail += string(b)
			if len(tail) > 4 {
				tail = tail[len(tail)-4:]
			}
			if strings.EqualFold(tail, "cmd:") {
				return nil
			}
		}
		return fmt.Errorf("command prompt not found within 8192 bytes")
	}
	// The binary KISS return handles a radio already in KISS after reconnect.
	// Ctrl-C leaves converse mode before sending any CR; the second Ctrl-C
	// clears text left at the command prompt by the binary return sequence.
	steps := []struct {
		command, name string
		expectPrompt  bool
	}{
		{"\x03\xc0\xff\xc0\x03\r", "command mode", true},
		{"KISS ON\r", "KISS ON", true},
		{"RESTART\r", "RESTART", false},
	}
	for _, step := range steps {
		err := write(step.command)
		if err == nil && step.expectPrompt {
			err = prompt()
		}
		if err != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			return nil, fmt.Errorf("Kenwood %s: %w", step.name, err)
		}
	}
	timer := time.NewTimer(settle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("Kenwood restart: %w", ctx.Err())
	case <-timer.C:
	}
	return &bufferedPort{ReadWriteCloser: port, reader: reader}, nil
}

type bufferedPort struct {
	io.ReadWriteCloser
	reader *bufio.Reader
}

func (p *bufferedPort) Read(data []byte) (int, error) { return p.reader.Read(data) }
