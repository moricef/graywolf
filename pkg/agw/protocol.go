// Package agw implements a minimal AGWPE-compatible TCP server. The wire
// format matches direwolf's server.c so existing clients (APRSIS32, UI-View,
// YAAC, Xastir) interoperate without modification.
//
//	Client → server:
//	  'R'  query AGW version
//	  'G'  query port information (list of radio ports)
//	  'g'  query port capabilities
//	  'X'  register callsign
//	  'x'  unregister callsign
//	  'm'  start monitoring (enable 'U'-type rx packets)
//	  'k'  toggle raw AX.25 frame reception (each 'k' flips it on/off)
//	  'M'  transmit UNPROTO (UI) frame — server must build the AX.25 header
//	  'V'  transmit UNPROTO (UI) frame via digipeaters
//	  'C'  connect to a remote station
//	  'v'  connect to a remote station via digipeaters
//	  'D'  transmit connected-mode data
//	  'd'  disconnect from a remote station
//	  'Y'  query frames outstanding on a connection
//
//	Server → client:
//	  'R'  version response
//	  'G'  port info response
//	  'g'  port capability response
//	  'X'  callsign registered ack
//	  'U'  monitored UI frame from RF
//	  'K'  raw AX.25 frame from RF, for clients that enabled it via 'k'
//	  'C'  connection established
//	  'D'  received connected-mode data
//	  'd'  disconnect notification, also used to refuse a connect
//	  'Y'  outstanding frame count
//
// Not implemented: 'c' (connect with a non-standard PID) — ax25conn has
// no per-session PID, so I-frames always go out as 0xF0. 'y' (frames
// outstanding on a port, as opposed to 'Y' on a connection).
//
// Connected mode is outbound only: an AGW client can raise a link to a
// remote station, but an incoming SABM from RF is not offered to AGW
// clients.
package agw

import (
	"github.com/chrissnell/graywolf/pkg/agw/wire"
	"io"
)

const HeaderSize = wire.HeaderSize

type Header = wire.Header

const (
	KindVersion            = wire.KindVersion
	KindPortInfo           = wire.KindPortInfo
	KindPortCaps           = wire.KindPortCaps
	KindRegisterCallsign   = wire.KindRegisterCallsign
	KindUnregisterCallsign = wire.KindUnregisterCallsign
	KindMonitorOn          = wire.KindMonitorOn
	KindSendUnproto        = wire.KindSendUnproto
	KindSendUnprotoVia     = wire.KindSendUnprotoVia
	KindSendRaw            = wire.KindSendRaw
	KindToggleRawKISS      = wire.KindToggleRawKISS
	KindMonitoredUI        = wire.KindMonitoredUI
	KindConnect            = wire.KindConnect
	KindConnectVia         = wire.KindConnectVia
	KindDisconnect         = wire.KindDisconnect
	KindConnectedData      = wire.KindConnectedData
	KindOutstandingFrames  = wire.KindOutstandingFrames
)

func EncodeHeader(h *Header) []byte                        { return wire.EncodeHeader(h) }
func DecodeHeader(b []byte) (*Header, error)               { return wire.DecodeHeader(b) }
func ReadFrame(r io.Reader) (*Header, []byte, error)       { return wire.ReadFrame(r) }
func WriteFrame(w io.Writer, h *Header, data []byte) error { return wire.WriteFrame(w, h, data) }
func trimNul(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return s[:i]
		}
	}
	return s
}
