package configstore

import "fmt"

// ValidateTCPProtocol is shared by HTTP validation and direct store callers.
// AGWPE clients are APRS radio interfaces, never application-side modems.
func ValidateTCPProtocol(typ, mode, protocol string, port uint8, connected bool) error {
	if protocol != "" && protocol != "kiss" && protocol != "agwpe" {
		return fmt.Errorf("tcp_protocol must be empty, kiss, or agwpe")
	}
	if protocol != "" && typ != KissTypeTCPClient {
		return fmt.Errorf("tcp_protocol requires a TCP client interface")
	}
	if protocol == "agwpe" {
		if mode != "" && mode != KissModeTnc {
			return fmt.Errorf("AGWPE client requires TNC mode")
		}
		if connected {
			return fmt.Errorf("AGWPE client currently supports APRS UI frames only")
		}
		// The raw K payload contains a four-bit radio-port prefix.
		if port > 15 {
			return fmt.Errorf("agw_port must be 0-15 (AGWPE Port 1-16)")
		}
	} else if port != 0 {
		return fmt.Errorf("agw_port requires tcp_protocol=agwpe")
	}
	return nil
}
