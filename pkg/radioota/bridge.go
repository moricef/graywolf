package radioota

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"go.bug.st/serial"
)

type otaPort interface {
	io.ReadWriteCloser
	SetMode(*serial.Mode) error
	SetReadTimeout(time.Duration) error
	ResetInputBuffer() error
	Drain() error
}
type engineClock interface {
	Now() time.Time
	Sleep(time.Duration)
}
type wallClock struct{}

func (wallClock) Now() time.Time        { return time.Now() }
func (wallClock) Sleep(d time.Duration) { time.Sleep(d) }

type bridge struct {
	port   otaPort
	clock  engineClock
	buffer []byte
}

func openOTAPort(path string) (otaPort, error) {
	return serial.Open(path, &serial.Mode{BaudRate: 115200, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{RTS: false, DTR: false}})
}
func (b *bridge) write(s string) error {
	data := []byte(s)
	for len(data) > 0 {
		n, err := b.port.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

// The port read timeout keeps deadlines and cooperative pause responsive without
// a detached read goroutine. Limit console lines so noise cannot grow memory.
func (b *bridge) line() (string, error) {
	for {
		if i := bytes.IndexByte(b.buffer, '\n'); i >= 0 {
			s := strings.TrimSpace(string(b.buffer[:i]))
			b.buffer = b.buffer[i+1:]
			return s, nil
		}
		buf := make([]byte, 1024)
		n, err := b.port.Read(buf)
		if n > 0 {
			b.buffer = append(b.buffer, buf[:n]...)
			if len(b.buffer) > 8192 {
				b.buffer = nil
				return "", errors.New("OTA bridge console line exceeds 8192 bytes")
			}
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", nil
		}
	}
}
func (b *bridge) command(command, expected string, timeout, retry time.Duration) error {
	end := b.clock.Now().Add(timeout)
	next := time.Time{}
	for b.clock.Now().Before(end) {
		if !b.clock.Now().Before(next) {
			if err := b.write(command + "\n"); err != nil {
				return err
			}
			next = b.clock.Now().Add(retry)
		}
		line, err := b.line()
		if err != nil {
			return err
		}
		if line == expected {
			return nil
		}
		if strings.HasSuffix(line, " ERROR") {
			return fmt.Errorf("controller rejected %s", command)
		}
	}
	return fmt.Errorf("%w: bridge %s", errRadioTimeout, command)
}
func openBridge(path string, open func(string) (otaPort, error), clock engineClock) (*bridge, error) {
	p, err := open(path)
	if err != nil {
		return nil, err
	}
	b := &bridge{port: p, clock: clock}
	fail := func(e error) (*bridge, error) { p.Close(); return nil, e }
	if err = p.SetReadTimeout(100 * time.Millisecond); err != nil {
		return fail(err)
	}
	if err = p.ResetInputBuffer(); err != nil {
		return fail(err)
	}
	if err = b.command("OTABRIDGE 921600", "OTABRIDGE OK", 15*time.Second, time.Second); err != nil {
		if !errors.Is(err, errRadioTimeout) {
			return fail(err)
		}
		// A prior interrupted client may have left the UART at the transfer baud.
		// Recover without asserting reset lines or opening a second serial handle.
		if err = p.SetMode(&serial.Mode{BaudRate: 921600, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}); err != nil {
			return fail(err)
		}
		if err = p.ResetInputBuffer(); err != nil {
			return fail(err)
		}
		b.buffer = nil
		if err = b.command("OTABRIDGE 921600", "OTABRIDGE OK", 5*time.Second, time.Second); err != nil {
			return fail(err)
		}
	}
	if err = p.SetMode(&serial.Mode{BaudRate: 921600, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}); err != nil {
		return fail(err)
	}
	clock.Sleep(time.Second)
	if err = p.ResetInputBuffer(); err != nil {
		return fail(err)
	}
	b.buffer = nil
	return b, nil
}
func (p Profile) airtime(length int) time.Duration {
	sf := float64(p.SF)
	symbol := math.Exp2(sf) / float64(p.Bandwidth)
	de := 0.0
	if symbol >= .016 {
		de = 1
	}
	payload := 8 + math.Max(math.Ceil((8*float64(length)-4*sf+44)/(4*(sf-2*de)))*float64(p.CR), 0)
	return time.Duration((12.25 + payload) * symbol * float64(time.Second))
}
