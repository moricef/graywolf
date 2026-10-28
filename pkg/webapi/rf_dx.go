package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/rfdx"
)

// RegisterRFDX exposes RF heard counters and direct DX even with APRS-IS off.
func RegisterRFDX(srv *Server, mux *http.ServeMux, tracker *rfdx.Tracker) {
	if srv == nil || mux == nil {
		return
	}
	mux.HandleFunc("GET /api/rf/dx", getRFDX(tracker))
}

// getRFDX returns per-channel heard counters, downlinked message totals and DX.
// @Summary Get iGate statistics and direct RF DX
// @Tags igate
// @ID getRFDX
// @Produce json
// @Success 200 {object} rfdx.Snapshot
// @Security CookieAuth
// @Router /rf/dx [get]
func getRFDX(tracker *rfdx.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tracker.Snapshot())
	}
}
