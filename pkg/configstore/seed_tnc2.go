package configstore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"gorm.io/gorm"
)

func (c TNC2Config) Validate() error {
	if c.TCPAddress != "" {
		host, port, err := net.SplitHostPort(c.TCPAddress)
		if err != nil || host == "" || port == "" {
			return errors.New("TCP address must be host:port")
		}
	}
	if c.SerialDevice != "" && c.SerialBaud == 0 {
		return errors.New("serial baud rate must be nonzero")
	}
	if c.TXTransport == "" {
		return nil
	}
	if c.MaxTXBytes == 0 || c.MaxTXBytes > 65536 || c.TXChannel == 0 || c.TXSource == "" {
		return errors.New("transmission requires a source, channel and peer byte limit")
	}
	if strings.ContainsAny(c.TXSource, ">,:* \r\n") {
		return errors.New("transmit source is not a textual address")
	}
	switch c.TXTransport {
	case "tcp":
		if c.TCPAddress == "" {
			return errors.New("TCP transmission requires a TCP address")
		}
	case "serial":
		if c.SerialDevice == "" {
			return errors.New("serial transmission requires a serial device")
		}
	default:
		return fmt.Errorf("unknown transmit transport %q", c.TXTransport)
	}
	return nil
}

func (s *Store) GetTNC2Config(ctx context.Context) (TNC2Config, bool, error) {
	var c TNC2Config
	err := s.db.WithContext(ctx).First(&c, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return TNC2Config{}, false, nil
	}
	return c, err == nil, err
}

func (s *Store) UpsertTNC2Config(ctx context.Context, c TNC2Config) error {
	c.TCPAddress = strings.TrimSpace(c.TCPAddress)
	c.SerialDevice = strings.TrimSpace(c.SerialDevice)
	c.TXSource = strings.TrimSpace(c.TXSource)
	if err := c.Validate(); err != nil {
		return err
	}
	c.ID = 1
	return s.db.WithContext(ctx).Save(&c).Error
}
