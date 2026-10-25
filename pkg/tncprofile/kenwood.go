package tncprofile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// The radio-control command sets differ even though both TNCs accept KISS ON.
// D700: https://www.on7lds.net/auto/tmd700a.htm (N0RMO's mode experiments).
// D710: Kenwood TM-D710AE_IDM_R1.pdf, section 5.1.3; TN mode/band query
// documented by LA3QMA/TM-V71_TM-D710-Kenwood, commands/TN.md.
func initializeModel(parent context.Context, port io.ReadWriteCloser, profile string, settle time.Duration) (result io.ReadWriteCloser, err error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stopClose()
	defer func() {
		if err != nil {
			_ = port.Close()
			if ctx.Err() != nil {
				err = fmt.Errorf("%s initialization: %w", profile, ctx.Err())
			}
		}
	}()
	r := bufio.NewReader(port)
	write := func(s string) error {
		n, err := io.WriteString(port, s)
		if err == nil && n != len(s) {
			err = io.ErrShortWrite
		}
		return err
	}
	pause := func(delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	// No CR is sent until control characters have escaped converse/transparent
	// mode. KISS return is binary; it is never sent as a printable command.
	if err = write("\x03\x03\x03\xc0\xff\xc0"); err != nil {
		return nil, err
	}
	if err = pause(200 * time.Millisecond); err != nil {
		return nil, err
	}
	if err = write("\x03\x03\x03\r"); err != nil {
		return nil, err
	}
	// Discard only the synchronization transcript. A stale cmd: must never
	// acknowledge TC 1: require its distinct TS 1 radio-control reply.
	if err = write("TC 1\r"); err != nil {
		return nil, err
	}
	if _, err = readKenwoodResponse(r, false, func(s string) bool { return s == "TS 1" }); err != nil {
		return nil, fmt.Errorf("%s TC 1: %w", profile, err)
	}
	if err = write("ID\r"); err != nil {
		return nil, err
	}
	model, err := readKenwoodResponse(r, true, func(s string) bool { return strings.HasPrefix(s, "ID ") })
	if err != nil {
		return nil, fmt.Errorf("%s ID: %w", profile, err)
	}
	expected := "ID TM-D700"
	if profile == TMD710 {
		expected = "ID TM-D710"
	}
	suffix := strings.TrimPrefix(model, expected)
	allowedSuffix := suffix == "" || suffix == "A" || suffix == "E" || (profile == TMD710 && (suffix == "G" || suffix == "GA" || suffix == "GE"))
	if !strings.HasPrefix(model, expected) || !allowedSuffix {
		return nil, fmt.Errorf("%s: unexpected radio model %q", profile, model)
	}
	command := func(request string, prompt bool, match func(string) bool) error {
		if err := write(request + "\r"); err != nil {
			return err
		}
		if prompt {
			_, err := readKenwoodPrompt(r, request == "KISS ON")
			return err
		}
		_, err := readKenwoodResponse(r, true, match)
		return err
	}
	if profile == TMD700 {
		for _, request := range []string{"AI 1", "TNC 2"} {
			if err = command(request, false, func(s string) bool { return s == request }); err != nil {
				return nil, fmt.Errorf("TM-D700 %s: %w", request, err)
			}
		}
		if err = command("TC 0", true, nil); err != nil {
			return nil, fmt.Errorf("TM-D700 TC 0: %w", err)
		}
	} else {
		if err = write("TN\r"); err != nil {
			return nil, err
		}
		state, err := readKenwoodResponse(r, true, func(s string) bool { return strings.HasPrefix(s, "TN ") })
		if err != nil {
			return nil, fmt.Errorf("TM-D710 TN: %w", err)
		}
		parts := strings.Split(strings.TrimPrefix(state, "TN "), ",")
		if len(parts) != 2 || len(parts[0]) != 1 || !strings.Contains("012", parts[0]) || len(parts[1]) != 1 || !strings.Contains("0123", parts[1]) {
			return nil, fmt.Errorf("TM-D710: invalid TNC mode/band %q", state)
		}
		request := "TN 2," + parts[1]
		if err = command(request, false, func(s string) bool { return s == request }); err != nil {
			return nil, fmt.Errorf("TM-D710 %s: %w", request, err)
		}
		if err = command("", true, nil); err != nil {
			return nil, fmt.Errorf("TM-D710 PACKET prompt: %w", err)
		}
	}
	if err = command("KISS ON", true, nil); err != nil {
		return nil, fmt.Errorf("%s KISS ON: %w", profile, err)
	}
	if err = write("RESTART\r"); err != nil {
		return nil, err
	}
	stream := newRestartPort(port, r)
	if err = stream.waitRestart(ctx, settle); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("%s RESTART: %w", profile, err)
	}
	// KISS has no mandatory startup acknowledgement. This means the serial
	// session is ready, not that an RF exchange has been validated.
	return stream, nil
}

func kenwoodRejected(line string) bool {
	line = strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(line, "?") || line == "N" || strings.HasPrefix(line, "UNKNOWN") || strings.HasPrefix(line, "ERROR")
}

func readKenwoodResponse(r *bufio.Reader, reject bool, match func(string) bool) (string, error) {
	line := ""
	for i := 0; i < 8192; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\r' || b == '\n' {
			text := strings.TrimSpace(line)
			line = ""
			if reject && kenwoodRejected(text) {
				return "", fmt.Errorf("radio rejected command: %s", text)
			}
			if match(text) {
				return text, nil
			}
		} else if b >= 32 && b < 127 {
			line += string(b)
		}
		if strings.HasSuffix(strings.ToLower(line), "cmd:") {
			before := strings.TrimSpace(line[:len(line)-4])
			if reject && kenwoodRejected(before) {
				return "", fmt.Errorf("radio rejected command: %s", before)
			}
			line = ""
		}
	}
	return "", fmt.Errorf("expected radio response not found within 8192 bytes")
}

func readKenwoodPrompt(r *bufio.Reader, requireKISS bool) (string, error) {
	line := ""
	acknowledged := !requireKISS
	for i := 0; i < 8192; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\r' || b == '\n' {
			if kenwoodRejected(line) {
				return "", fmt.Errorf("radio rejected command: %s", strings.TrimSpace(line))
			}
			if kenwoodKISSAcknowledgement(line) {
				acknowledged = true
			}
			line = ""
		} else if b >= 32 && b < 127 {
			line += string(b)
		}
		if strings.HasSuffix(strings.ToLower(line), "cmd:") {
			before := line[:len(line)-4]
			if kenwoodRejected(before) {
				return "", fmt.Errorf("radio rejected command: %s", before)
			}
			if kenwoodKISSAcknowledgement(before) {
				acknowledged = true
			}
			if acknowledged {
				return line, nil
			}
			line = "" // A stale startup prompt does not acknowledge KISS ON.
		}
	}
	return "", fmt.Errorf("command prompt not found within 8192 bytes")
}

func kenwoodKISSAcknowledgement(line string) bool {
	switch strings.ToUpper(strings.TrimSpace(line)) {
	case "KISS ON", "KISS WAS OFF", "KISS WAS ON":
		return true
	default:
		return false
	}
}
