package radioota

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSF5SF6Settings(t *testing.T) {
	for _, sf := range []int{4, 5, 6, 7, 12, 13} {
		p := settings().Transfer
		p.SF = sf
		if p.Valid() != (sf >= 5 && sf <= 12) {
			t.Fatalf("SF%d validity wrong", sf)
		}
	}
	for _, model := range []string{"ttgo-lora32-v21", "heltec-v3", "qrp-labs-lightgateway-plus"} {
		for _, sf := range []int{5, 6, 7} {
			cfg := settings()
			cfg.TargetBoard = model
			cfg.Normal.SF = sf
			cfg.Transfer.SF = sf
			cfg.Transfer.Bandwidth = 500000
			err := cfg.Validate()
			if model == "ttgo-lora32-v21" && sf < 7 {
				if err == nil || !strings.Contains(err.Error(), "SX1278") {
					t.Fatalf("TTGO SF%d accepted: %v", sf, err)
				}
			} else if err != nil {
				t.Fatalf("%s SF%d: %v", model, sf, err)
			}
		}
	}
	cfg := settings()
	cfg.Normal.SF = 5
	cfg.Via = "F4MLV-10"
	cfg.RouteSecret = secret
	target := cfg.Normal
	target.SF = 7
	target.Frequency = 434100000
	cfg.TargetNormal = &target
	if err := cfg.Validate(); err != nil {
		t.Fatalf("TTGO target on its SF7 service must not force SX126x controller service to SF7: %v", err)
	}
}

func sf5Args(t *testing.T, p *preparedPort) []string {
	t.Helper()
	args := nativeArgs(t, p.fakeOTA)
	normal := settings().Normal
	normal.SF = 7
	transfer := Profile{434500000, 500000, 5, 5, 9}
	b, err := json.Marshal(map[string]Profile{"normal": normal, "transfer": transfer})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(arg(args, "--radio-profiles"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return append(args, "--prepare-route", "--recipient", p.rfTarget, "--relay-recipient", p.rfRelay)
}

func TestSF5CompleteCycles(t *testing.T) {
	for _, board := range []uint32{0x484c5633, 0x51524c50} {
		for _, relay := range []string{"", "F4MLV-10"} {
			p := preparedFake(board, relay)
			args := sf5Args(t, p)
			var log bytes.Buffer
			err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
			if err != nil {
				t.Fatalf("board %x relay %q: %v\n%s", board, relay, err, log.String())
			}
			if !strings.Contains(log.String(), "post_install_verified") || !p.bridgeOff {
				t.Fatal(log.String())
			}
			found := false
			for _, raw := range p.txFrames {
				if relay != "" {
					raw = raw[13 : len(raw)-16]
				}
				if raw[4] == opPrepare {
					found = true
					if raw[46] != 5 || raw[47] != 5 {
						t.Fatal("negotiated SF/CR not 5/5")
					}
				}
			}
			if !found {
				t.Fatal("fast profile was not negotiated")
			}
			if relay != "" && !strings.Contains(strings.Join(p.commands, "\n"), p.rfRelay+":OTA=PROFILE,434500000,500000,5,5,9") {
				t.Fatal("relay was not checked")
			}
		}
	}
}

func TestSF5UnsupportedRemoteRefusedBeforeBlocks(t *testing.T) {
	for _, relay := range []string{"", "F4MLV-10"} {
		p := preparedFake(0x51524c50, relay)
		p.fault = "unsupported-profile"
		args := sf5Args(t, p)
		var log bytes.Buffer
		err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
		if err == nil || !strings.Contains(err.Error(), "SF5") {
			t.Fatalf("unsupported remote accepted: %v", err)
		}
		if p.manifestCount != 0 || len(p.dataOffsets) != 0 || !p.bridgeOff {
			t.Fatal("blocks sent or controller not released")
		}
	}
}

func TestSF5EveryRelayCheckedAndPreflightPreservesCandidate(t *testing.T) {
	for _, unsupported := range []string{"", "RELAYY", "GENATX"} {
		p := preparedFake(0x51524c50, "F4MLV-10")
		p.chainRF = map[string]string{"RELAYY": "F4MLV-11"}
		p.unsupportedProfileRF = unsupported
		p.committed = 4096
		args := append(sf5Args(t, p), "--chain", "F4MLV-11:RELAYY", "--preflight-only")
		var log bytes.Buffer
		err := runNative(args, &log, func(string) (otaPort, error) { return p, nil }, p.clock, preparationConfig{Signer: p.sign})
		if unsupported != "" {
			if err == nil || !strings.Contains(err.Error(), unsupported) {
				t.Fatalf("%s not refused: %v", unsupported, err)
			}
		} else {
			if err != nil {
				t.Fatal(err, log.String())
			}
			for _, rf := range []string{p.rfRelay, "RELAYY", p.rfTarget} {
				if !strings.Contains(strings.Join(p.commands, "\n"), rf+":OTA=PROFILE,") {
					t.Fatalf("%s was not checked", rf)
				}
			}
		}
		if p.committed != 4096 || p.manifestCount != 0 || len(p.dataOffsets) != 0 {
			t.Fatal("preflight changed candidate")
		}
		for _, op := range p.ops {
			if op == opAbort {
				t.Fatal("preflight sent ABORT")
			}
		}
	}
}

type rejectingSF5Port struct{ *preparedPort }

func (p rejectingSF5Port) Write(b []byte) (int, error) {
	if strings.HasPrefix(string(b), "PROFILEDEF 1 ") {
		p.rx.WriteString("PROFILEDEF 1 ERROR\n")
		return len(b), nil
	}
	return p.preparedPort.Write(b)
}
func TestSF5UnsupportedControllerRefusedBeforeRadio(t *testing.T) {
	p := preparedFake(0x51524c50, "")
	args := sf5Args(t, p)
	var log bytes.Buffer
	err := runNative(args, &log, func(string) (otaPort, error) { return rejectingSF5Port{p}, nil }, p.clock, preparationConfig{Signer: p.sign})
	if err == nil || !strings.Contains(err.Error(), "SX126x USB radio") {
		t.Fatal(err)
	}
	if len(p.commands) != 0 || len(p.txFrames) != 0 || !p.bridgeOff {
		t.Fatal("radio used after controller profile rejection")
	}
}

var _ io.Writer = rejectingSF5Port{}
