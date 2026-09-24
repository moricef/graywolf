package tnc2link

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/backoff"
	"github.com/chrissnell/graywolf/pkg/tnc2"
	"go.bug.st/serial"
)

const (
	TransportTCP    = "tcp"
	TransportSerial = "serial"
)

// ClientConfig selects exactly one TNC2 transport. OpenFunc is intended for
// transport tests and platform-specific serial implementations.
type ClientConfig struct {
	Transport        string
	Address          string // host:port when Transport == tcp
	Device           string // serial device when Transport == serial
	BaudRate         uint32
	MaxTransmitBytes int // optional peer limit; CA2RXU TNC2 input is 255 bytes
	OpenFunc         func(context.Context) (io.ReadWriteCloser, error)
	OnPacket         func(*tnc2.TNC2Packet)
	OnSent           func([]byte)
	OnSendError      func(error)
}

// Client supervises one bidirectional TNC2 link. It never sends on connect:
// TX requires an explicit SendPacket call from an authorized caller.
type Client struct {
	cfg     ClientConfig
	mu      sync.RWMutex
	link    *Link
	txQueue chan []byte
}

func NewClient(cfg ClientConfig) *Client {
	return &Client{cfg: cfg, txQueue: make(chan []byte, 16)}
}

func (c *Client) Run(ctx context.Context) error {
	if c.cfg.OnPacket == nil {
		return errors.New("tnc2 client: nil packet callback")
	}
	if c.cfg.OpenFunc == nil {
		switch c.cfg.Transport {
		case TransportTCP:
			if c.cfg.Address == "" {
				return errors.New("tnc2 client: empty TCP address")
			}
		case TransportSerial:
			if c.cfg.Device == "" || c.cfg.BaudRate == 0 {
				return errors.New("tnc2 client: serial device and baud rate required")
			}
		default:
			return fmt.Errorf("tnc2 client: unsupported transport %q", c.cfg.Transport)
		}
	}
	bo := backoff.New(backoff.Config{Initial: time.Second, Max: time.Minute})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-c.txQueue:
				if err := c.SendPacket(ctx, raw); err != nil {
					if c.cfg.OnSendError != nil {
						c.cfg.OnSendError(err)
					}
				} else if c.cfg.OnSent != nil {
					c.cfg.OnSent(raw)
				}
			}
		}
	}()
	defer func() { <-writerDone }()
	for ctx.Err() == nil {
		conn, err := c.open(ctx)
		if err == nil {
			bo.Reset()
			link := NewLimitedLink(conn, c.cfg.MaxTransmitBytes)
			c.mu.Lock()
			c.link = link
			c.mu.Unlock()
			_ = link.ReadPackets(ctx, c.cfg.OnPacket)
			c.mu.Lock()
			c.link = nil
			c.mu.Unlock()
			_ = link.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(bo.Next())
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
	return nil
}

func (c *Client) SendPacket(ctx context.Context, raw []byte) error {
	c.mu.RLock()
	link := c.link
	c.mu.RUnlock()
	if link == nil {
		return ErrDisconnected
	}
	return link.SendPacket(ctx, raw)
}

// EnqueuePacket is the non-blocking TX entry used by the governor. The
// caller is told immediately if the peer is down, the record is invalid,
// or the bounded writer queue is full.
func (c *Client) EnqueuePacket(raw []byte) error {
	if err := ValidatePacket(raw, c.cfg.MaxTransmitBytes); err != nil {
		return err
	}
	if !c.Connected() {
		return ErrDisconnected
	}
	select {
	case c.txQueue <- bytes.Clone(raw):
		return nil
	default:
		return ErrQueueFull
	}
}

func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.link != nil
}

func (c *Client) open(ctx context.Context) (io.ReadWriteCloser, error) {
	if c.cfg.OpenFunc != nil {
		return c.cfg.OpenFunc(ctx)
	}
	if c.cfg.Transport == TransportTCP {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", c.cfg.Address)
	}
	return serial.Open(c.cfg.Device, &serial.Mode{
		BaudRate: int(c.cfg.BaudRate),
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
}
