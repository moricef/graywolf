package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/rfdx"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func RegisterDX(s *Server, mux *http.ServeMux, apply func(rfdx.Config), send func(context.Context, string) error, reset func(context.Context) error) {
	mux.HandleFunc("GET /api/dx/config", s.getDXConfig)
	mux.HandleFunc("PUT /api/dx/config", func(w http.ResponseWriter, r *http.Request) { s.updateDXConfig(w, r, apply) })
	mux.HandleFunc("POST /api/dx/reports/{kind}", func(w http.ResponseWriter, r *http.Request) { s.sendDXReport(w, r, send) })
	mux.HandleFunc("DELETE /api/dx/record", func(w http.ResponseWriter, r *http.Request) { s.resetDXRecord(w, r, reset) })
}

// @Summary Get DX and station report configuration
// @Tags dx
// @ID getDXConfig
// @Produce json
// @Success 200 {object} rfdx.Config
// @Security CookieAuth
// @Router /dx/config [get]
func (s *Server) getDXConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetDXSettings(r.Context())
	if err != nil {
		s.internalError(w, r, "get DX settings", err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// @Summary Configure DX detection and periodic station reports
// @Tags dx
// @ID updateDXConfig
// @Accept json
// @Produce json
// @Param body body rfdx.Config true "DX and station report settings"
// @Success 200 {object} rfdx.Config
// @Security CookieAuth
// @Router /dx/config [put]
func (s *Server) updateDXConfig(w http.ResponseWriter, r *http.Request, apply func(rfdx.Config)) {
	c, err := decodeJSON[rfdx.Config](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err = c.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	if err = dto.ValidateChannelRef(r.Context(), s.store, "channel", c.Channel); err != nil {
		badRequest(w, err.Error())
		return
	}
	req := dto.BeaconRequest{Type: "custom", Channel: c.Channel, Destination: "APGRWO", Path: c.Path, CustomInfo: ">", SendPath: c.SendPath}
	if err = s.validateBeaconAddresses(req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if c.SendPath != "is_only" && (c.StatusEnabled || c.CapabilitiesEnabled) {
		if err = s.requireBeaconTxCapableChannel(r.Context(), c.Channel); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if err = s.store.SaveDXSettings(r.Context(), c); err != nil {
		s.internalError(w, r, "save DX settings", err)
		return
	}
	if apply != nil {
		apply(c)
	}
	writeJSON(w, http.StatusOK, c)
}

// @Summary Send a station report now
// @Tags dx
// @ID sendDXReport
// @Produce json
// @Param kind path string true "status or capabilities"
// @Success 200 {object} map[string]bool
// @Security CookieAuth
// @Router /dx/reports/{kind} [post]
func (s *Server) sendDXReport(w http.ResponseWriter, r *http.Request, send func(context.Context, string) error) {
	kind := r.PathValue("kind")
	if kind != "status" && kind != "capabilities" {
		badRequest(w, "report kind must be status or capabilities")
		return
	}
	if send == nil {
		badRequest(w, "station reports unavailable")
		return
	}
	if err := send(r.Context(), kind); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

// @Summary Clear the all-time DX record
// @Tags dx
// @ID resetDXRecord
// @Produce json
// @Success 200 {object} map[string]bool
// @Security CookieAuth
// @Router /dx/record [delete]
func (s *Server) resetDXRecord(w http.ResponseWriter, r *http.Request, reset func(context.Context) error) {
	if reset == nil {
		badRequest(w, "DX records unavailable")
		return
	}
	if err := reset(r.Context()); err != nil {
		s.internalError(w, r, "reset DX record", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}
