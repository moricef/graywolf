package dto

import "testing"

func TestAGWClientSettings(t *testing.T) {
	req := KissRequest{Type: "tcp-client", RemoteHost: "127.0.0.1", RemotePort: 8000, TCPProtocol: "agwpe", AgwPort: 1}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	model := req.ToModel()
	out := KissFromModel(model)
	if model.Mode != "tnc" || !model.AllowTxFromGovernor || out.TCPProtocol != "agwpe" || out.AgwPort != 1 {
		t.Fatalf("bad defaults or roundtrip: %+v", out)
	}
	for _, change := range []func(*KissRequest){
		func(r *KissRequest) { r.TCPProtocol = "unknown" },
		func(r *KissRequest) { r.Mode = "modem" },
		func(r *KissRequest) { r.Type = "serial"; r.SerialDevice = "/dev/test"; r.BaudRate = 9600 },
		func(r *KissRequest) { r.AgwPort = 16 },
		func(r *KissRequest) { r.AllowConnectedMode = true },
		func(r *KissRequest) { r.TCPProtocol = "kiss" },
	} {
		bad := req
		change(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid settings accepted: %+v", bad)
		}
	}
}
