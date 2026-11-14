package radioota

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	opAbort          byte = 5
	opHello          byte = 6
	opConfirm        byte = 7
	opStatus         byte = 8
	opReactivate     byte = 10
	opPrepare        byte = 11
	opReturn         byte = 12
	opNetwork        byte = 14
	opWindowStart    byte = 15
	opWindowData     byte = 16
	opWindowQuery    byte = 17
	opWindowFinish   byte = 18
	opWindowActivate byte = 19
	opRadioState     byte = 32
	opRendezvous     byte = 33
	opMaintain       byte = 34
	opRelease        byte = 35
)

var otaPrefix = []byte{0x3c, 0xff, 0x7e, 2}
var routePrefix = []byte{0x3c, 0xff, 0x7d, 1}

func tag(key []byte, domain string, raw []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(domain))
	m.Write(raw)
	return m.Sum(nil)[:16]
}
func identity(s string) []byte { b := make([]byte, 8); copy(b, s); return b }
func frame(key []byte, source, target string, nonce [8]byte, op byte, offset uint32, payload []byte) ([]byte, error) {
	if len(key) != 32 || !address.MatchString(source) || !address.MatchString(target) || len(payload) > 204 {
		return nil, errors.New("invalid OTA frame parameters")
	}
	b := make([]byte, 35+len(payload))
	copy(b, otaPrefix)
	b[4] = op
	copy(b[5:13], nonce[:])
	binary.BigEndian.PutUint32(b[13:17], offset)
	binary.BigEndian.PutUint16(b[17:19], uint16(len(payload)))
	copy(b[19:27], identity(source))
	copy(b[27:35], identity(target))
	copy(b[35:], payload)
	return append(b, tag(key, "LORA-APRS-OTA2", b)...), nil
}

type radioReply struct {
	Nonce                      [8]byte
	Op                         byte
	Status                     byte
	Offset, Written            uint32
	Bitmap                     uint32
	HasBitmap                  bool
	Proof                      *BootProof
	WiFiMode                   byte
	Connected, ActiveInterface bool
	HasNetwork                 bool
	Radio                      *radioState
}

func decodeReply(key []byte, source, target string, b []byte) (radioReply, error) {
	var r radioReply
	if len(b) < 57 || !bytes.Equal(b[:4], otaPrefix) || b[4] != 128 || len(b) != 51+int(binary.BigEndian.Uint16(b[17:19])) || !bytes.Equal(b[19:27], identity(target)) || !bytes.Equal(b[27:35], identity(source)) || !hmac.Equal(b[len(b)-16:], tag(key, "LORA-APRS-OTA2", b[:len(b)-16])) {
		return r, errors.New("invalid authenticated OTA reply")
	}
	copy(r.Nonce[:], b[5:13])
	r.Offset = binary.BigEndian.Uint32(b[13:17])
	p := b[35 : len(b)-16]
	r.Op = p[0]
	r.Status = p[1]
	r.Written = binary.BigEndian.Uint32(p[2:6])
	switch r.Op {
	case opRadioState, opRendezvous, opMaintain, opRelease:
		if len(p) != 37 || p[6] != 1 || p[7] > 3 || p[8] > 1 || p[9] > 1 || p[10] > 1 {
			return r, errors.New("unsupported radio coordination proof")
		}
		r.Radio = &radioState{Phase: p[7], Armed: p[8] != 0, RouteBusy: p[9] != 0, Known: p[10] != 0, Service: decodeProfile(p[11:22]), Applied: decodeProfile(p[22:33]), Sequence: binary.BigEndian.Uint32(p[33:37])}
	case opWindowStart, opWindowQuery:
		if len(p) != 10 {
			return r, errors.New("unexpected bitmap width for 128-byte blocks")
		}
		r.HasBitmap = true
		r.Bitmap = binary.BigEndian.Uint32(p[6:10])
	case opNetwork:
		if len(p) != 9 || p[6] > 3 || p[7] > 1 || p[8] > 1 {
			return r, errors.New("invalid network status")
		}
		r.HasNetwork = true
		r.WiFiMode = p[6]
		r.Connected = p[7] != 0
		r.ActiveInterface = p[8] != 0
	default:
		if len(p) == 6 {
			return r, nil
		}
		if len(p) < 48 || p[6] > 1 {
			return r, errors.New("invalid boot status")
		}
		n := int(p[15])
		if n > 40 || len(p) != 48+n {
			return r, errors.New("invalid firmware version size")
		}
		for _, v := range p[16 : 16+n] {
			if v < 32 || v > 126 {
				return r, errors.New("invalid firmware version")
			}
		}
		pending := p[6] == 1
		r.Proof = &BootProof{Pending: &pending, Running: binary.BigEndian.Uint32(p[7:11]), Boot: binary.BigEndian.Uint32(p[11:15]), Version: string(p[16 : 16+n]), ImageSHA: hex.EncodeToString(p[16+n:])}
	}
	return r, nil
}
func wrapRoute(key []byte, source string, inner []byte) ([]byte, error) {
	if len(key) != 32 || len(inner) < 51 || len(inner) > 226 {
		return nil, errors.New("invalid route frame")
	}
	b := append(append(append([]byte{}, routePrefix...), 0), identity(source)...)
	b = append(b, inner...)
	return append(b, tag(key, "LORA-APRS-OTA-HOP1", b)...), nil
}
func unwrapRoute(key []byte, relay string, b []byte) ([]byte, error) {
	if len(b) < 80 || len(b) > 255 || !bytes.Equal(b[:4], routePrefix) || b[4] != 3 || !bytes.Equal(b[5:13], identity(relay)) || !hmac.Equal(b[len(b)-16:], tag(key, "LORA-APRS-OTA-HOP1", b[:len(b)-16])) {
		return nil, errors.New("invalid authenticated route reply")
	}
	inner := b[13 : len(b)-16]
	if len(inner) < 51 || !bytes.Equal(inner[:4], otaPrefix) || len(inner) != 51+int(binary.BigEndian.Uint16(inner[17:19])) {
		return nil, errors.New("invalid routed payload")
	}
	return inner, nil
}

type protocolError struct {
	Op, Status byte
	Offset     uint32
}

func (e *protocolError) Error() string {
	return fmt.Sprintf("OTA operation %d rejected: status=%d offset=%d", e.Op, e.Status, e.Offset)
}
