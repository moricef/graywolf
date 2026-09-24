package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

type TNC2ConfigStatus struct {
	configstore.TNC2Config
	TCPConnected    bool `json:"tcp_connected"`
	SerialConnected bool `json:"serial_connected"`
}

// RegisterTNC2Config exposes the receive links and the separately authorized
// transmit link. Saving the settings applies them immediately and persists them.
func RegisterTNC2Config(mux *http.ServeMux, get func() TNC2ConfigStatus, save func(context.Context, configstore.TNC2Config) error) {
	mux.HandleFunc("GET /api/tnc2/config", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, get())
	})
	mux.HandleFunc("PUT /api/tnc2/config", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := decodeJSON[configstore.TNC2Config](r)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := cfg.Validate(); err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := save(r.Context(), cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, get())
	})
}
