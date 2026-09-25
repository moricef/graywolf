package app

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestBeaconConfigTextualIdentityHasNoAX25Dependency(t *testing.T) {
	for _, source := range []string{"F4MLV-16", "F4MLV-GS", "F4MLV-01"} {
		row := configstore.Beacon{Type: "position", Channel: 1, Callsign: source,
			Destination: "APGRWO", Path: "WIDE1-1", Latitude: 42.9, Longitude: 1.2}
		cfg, err := beaconConfigFromStoreWithMode(row, nil, "", true)
		if err != nil {
			t.Fatalf("textual %s: %v", source, err)
		}
		if cfg.SourceText != source || cfg.Source.Call != "" {
			t.Fatalf("textual source=%q AX25=%+v", cfg.SourceText, cfg.Source)
		}
		if _, err := beaconConfigFromStoreWithMode(row, nil, "", false); err == nil {
			t.Fatalf("legacy AX.25 beacon accepted extended source %s", source)
		}
	}
}

func TestBeaconConfigTextualPathIsExact(t *testing.T) {
	row := configstore.Beacon{Type: "position", Callsign: "F4MLV-2", Destination: "APGRWO", Path: "NN7LE-GS,WIDE2-1"}
	cfg, err := beaconConfigFromStoreWithMode(row, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PathText) != 2 || cfg.PathText[0] != "NN7LE-GS" || cfg.PathText[1] != "WIDE2-1" {
		t.Fatalf("path=%q", cfg.PathText)
	}
	if len(cfg.Path) != 0 {
		t.Fatalf("extended path was converted to AX.25: %+v", cfg.Path)
	}
}
