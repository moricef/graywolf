package dto

import (
	"strings"
	"testing"
)

func TestBeaconRequest_Validate_PositionFormat(t *testing.T) {
	mkPos := func() BeaconRequest {
		return BeaconRequest{
			Type:           "position",
			UseGps:         true,
			PositionFormat: "compressed",
		}
	}

	cases := []struct {
		name    string
		mutate  func(*BeaconRequest)
		wantErr string // substring; "" means expect nil
	}{
		{"compressed_zero_amb_ok", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 0
		}, ""},
		{"compressed_with_amb_rejected", func(r *BeaconRequest) {
			r.PositionFormat = "compressed"
			r.Ambiguity = 1
		}, "ambiguity must be 0 when position_format is compressed"},
		{"uncompressed_ok", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 2
		}, ""},
		{"uncompressed_amb_too_high", func(r *BeaconRequest) {
			r.PositionFormat = "uncompressed"
			r.Ambiguity = 5
		}, "ambiguity must be 0..4"},
		{"mic_e_accepted", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
			r.Ambiguity = 2
		}, ""},
		{"mic_e_amb_too_high", func(r *BeaconRequest) {
			r.PositionFormat = "mic_e"
			r.Ambiguity = 5
		}, "ambiguity must be 0..4"},
		{"unknown_format", func(r *BeaconRequest) {
			r.PositionFormat = "bogus"
		}, "position_format must be one of"},
		{"empty_format_defaults_compressed", func(r *BeaconRequest) {
			r.PositionFormat = ""
		}, ""},
		{"object_format_ignored", func(r *BeaconRequest) {
			r.Type = "object"
			r.PositionFormat = "mic_e"
			r.Latitude = 37
			r.Longitude = -122
			r.UseGps = false
		}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := mkPos()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestBeaconRequest_SendPathDefaultsRF(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1} // SendPath empty
	m := r.ToModel()
	if m.SendPath != "rf" {
		t.Fatalf("empty send_path should normalize to rf, got %q", m.SendPath)
	}
}

func TestBeaconRequest_SendPathISOnly(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only"}
	m := r.ToModel()
	if m.SendPath != "is_only" {
		t.Fatalf("send_path = %q, want is_only", m.SendPath)
	}
}

func TestBeaconRequest_Validate_BadSendPath(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "carrier-pigeon"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for unknown send_path")
	}
}

func TestBeaconRequest_Validate_ISOnlyOK(t *testing.T) {
	r := BeaconRequest{Type: "custom", SendPath: "is_only", CustomInfo: ">status"}
	if err := r.Validate(); err != nil {
		t.Fatalf("is_only should validate, got %v", err)
	}
}

func TestBeaconRequest_Validate_CustomRequiresInfo(t *testing.T) {
	for _, info := range []string{"", "   "} {
		r := BeaconRequest{Type: "custom", SendPath: "rf", CustomInfo: info}
		if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "custom_info") {
			t.Fatalf("CustomInfo %q: Validate() = %v, want custom_info error", info, err)
		}
	}
	r := BeaconRequest{Type: "custom", SendPath: "rf", CustomInfo: ">status"}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid custom beacon rejected: %v", err)
	}
}

func TestBeaconRequest_Validate_WeatherSource(t *testing.T) {
	valid := BeaconRequest{
		Type: "weather", UseGps: true, SendPath: "rf",
		WeatherSource: "wxnow_file", WeatherPath: "/var/lib/weather/WxNow.txt",
		Comment: "ignored", CommentCmd: "/usr/local/bin/ignored",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid weather beacon rejected: %v", err)
	}
	for _, tc := range []struct {
		name, source, path, want string
	}{
		{"missing source", "", "/tmp/WxNow.txt", "weather_source"},
		{"unknown source", "davis", "/tmp/WxNow.txt", "weather_source"},
		{"missing path", "wxnow_file", " ", "weather_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			r.WeatherSource, r.WeatherPath = tc.source, tc.path
			if err := r.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want substring %q", err, tc.want)
			}
		})
	}
	m := valid.ToModel()
	if m.WeatherSource != valid.WeatherSource || m.WeatherPath != valid.WeatherPath {
		t.Fatalf("weather source fields lost: %+v", m)
	}
	if m.SymbolTable != "/" || m.Symbol != "_" || m.PositionFormat != "uncompressed" {
		t.Fatalf("weather APRS wire fields not normalized: %+v", m)
	}
	if m.Comment != "" || m.CommentCmd != "" {
		t.Fatalf("weather comment fields must be cleared: %+v", m)
	}
}

func strPtr(s string) *string { return &s }

func TestBeaconRequest_Validate_BadCallsignSSID(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only", Callsign: strPtr("NW5W-17")}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for callsign with SSID 17 (out of APRS range 0-15)")
	}
}

func TestBeaconRequest_Validate_GoodCallsign(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "is_only", Callsign: strPtr("NW5W-7")}
	if err := r.Validate(); err != nil {
		t.Fatalf("NW5W-7 should validate, got %v", err)
	}
}

func TestBeaconRequest_Validate_RejectsNoncanonicalAX25Identity(t *testing.T) {
	for _, test := range []struct {
		name, source, dest, path string
	}{
		{"source leading zero", "F4MLV-01", "APGRWO", "WIDE1-1"},
		{"source explicit zero", "F4MLV-0", "APGRWO", "WIDE1-1"},
		{"source repeated marker", "F4MLV-2*", "APGRWO", "WIDE1-1"},
		{"destination leading zero", "F4MLV-2", "APGRWO-01", "WIDE1-1"},
		{"destination repeated marker", "F4MLV-2", "APGRWO*", "WIDE1-1"},
		{"path leading zero", "F4MLV-2", "APGRWO", "WIDE1-01"},
		{"outgoing repeated path", "F4MLV-2", "APGRWO", "WIDE1-1*"},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1,
				Callsign: strPtr(test.source), Destination: test.dest, Path: test.path}
			if err := req.Validate(); err == nil {
				t.Fatalf("noncanonical beacon %+v accepted", req)
			}
		})
	}
}

func TestBeaconRequest_Validate_InheritCallsignSkipsParse(t *testing.T) {
	// nil/empty override = inherit station callsign; must not be parsed here.
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "rf"}
	if err := r.Validate(); err != nil {
		t.Fatalf("inherited callsign should validate, got %v", err)
	}
}

func TestBeaconRequest_Validate_BadPath(t *testing.T) {
	r := BeaconRequest{Type: "position", Latitude: 1, Longitude: 1, SendPath: "rf", Path: "WIDE1-1,BADCALLSIGN-99"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for invalid path element")
	}
}
