package webapi

import (
	"context"
	"encoding/base64"
	"net/http"
)

// RegisterTNC2TX exposes explicit operator-requested textual transmission.
// The containing /api/ mux supplies session authentication; the sink must
// independently enforce source, channel, peer limit and transport policy.
func RegisterTNC2TX(_ *Server, mux *http.ServeMux, send func(context.Context, []byte) error) {
	mux.HandleFunc("POST /api/tnc2/tx", func(w http.ResponseWriter, r *http.Request) {
		if send == nil {
			http.Error(w, "TNC2 transmission is unavailable", http.StatusServiceUnavailable)
			return
		}
		body, err := decodeJSON[struct {
			RawTNC2Base64 string `json:"raw_tnc2_base64"`
		}](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(body.RawTNC2Base64)
		if err != nil || len(raw) == 0 {
			badRequest(w, "raw_tnc2_base64 must encode one nonempty TNC2 packet")
			return
		}
		if err := send(r.Context(), raw); err != nil {
			badRequest(w, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	})
}
