package kiss

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	agw "github.com/chrissnell/graywolf/pkg/agw/wire"
)

// initializeAGW discovers ports before enabling raw reception. Deadlines and
// cancellation are owned by dial; a TCP accept alone is not a ready radio.
// Reference: AGWPE TCP/IP API Tutorial (LU7DID/SV2AGW), G/k/K frames:
// https://www.on7lds.net/42/sites/default/files/AGWPEAPI.HTM
func (c *Client) initializeAGW(conn net.Conn) error {
	if err := agw.WriteFrame(conn, &agw.Header{DataKind: agw.KindPortInfo}, nil); err != nil {
		return err
	}
	for {
		h, data, err := agw.ReadFrame(conn)
		if err != nil {
			return err
		}
		if h.DataKind != agw.KindPortInfo {
			continue
		}
		parts := strings.Split(strings.TrimRight(string(data), "\x00"), ";")
		count, err := strconv.Atoi(parts[0])
		if err != nil || count < 1 || count > 256 || len(parts) < count+1 {
			return fmt.Errorf("invalid AGWPE port list")
		}
		for i := 1; i <= count; i++ {
			if strings.TrimSpace(parts[i]) == "" {
				return fmt.Errorf("incomplete AGWPE port list")
			}
		}
		if int(c.cfg.AgwPort) >= count {
			return fmt.Errorf("AGWPE Port %d unavailable: server advertises %d ports", int(c.cfg.AgwPort)+1, count)
		}
		c.logger.Info("AGWPE radio selected", "port", int(c.cfg.AgwPort)+1, "description", parts[int(c.cfg.AgwPort)+1])
		// k is a toggle: send exactly once on each new connection.
		return agw.WriteFrame(conn, &agw.Header{DataKind: agw.KindToggleRawKISS}, nil)
	}
}

func (c *Client) agwFrameLoop(ctx context.Context, conn net.Conn) error {
	for {
		h, data, err := agw.ReadFrame(conn)
		if err != nil {
			return err
		}
		// Ignore monitor echoes and other radio ports: they are not RF RX on
		// this channel. Header.Port is authoritative; do not default to ch 1.
		if h.DataKind != agw.KindSendRaw || h.Port != c.cfg.AgwPort {
			continue
		}
		if len(data) < 2 {
			if c.cfg.OnDecodeError != nil {
				c.cfg.OnDecodeError()
			}
			continue
		}
		// First byte is the raw port/command prefix, not AX.25 or a flag.
		// Some compatible servers always use zero here; use Header.Port.
		c.handleFrame(ctx, conn.RemoteAddr().String(), &Frame{Port: 0, Command: CmdDataFrame, Data: data[1:]})
	}
}
