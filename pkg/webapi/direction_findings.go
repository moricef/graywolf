package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerDirectionFindings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/direction-findings", s.listDirectionFindings)
	mux.HandleFunc("POST /api/direction-findings", s.createDirectionFinding)
	mux.HandleFunc("GET /api/direction-findings/{id}", s.getDirectionFinding)
	mux.HandleFunc("PUT /api/direction-findings/{id}", s.updateDirectionFinding)
	mux.HandleFunc("DELETE /api/direction-findings/{id}", s.deleteDirectionFinding)
}

// @Summary  List manual direction-finding observations
// @Tags     direction-findings
// @ID       listDirectionFindings
// @Produce  json
// @Success  200 {array} dto.DirectionFindingResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /direction-findings [get]
func (s *Server) listDirectionFindings(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.DirectionFindingObservation](s, w, r, "list direction findings",
		s.store.ListDirectionFindingObservations, dto.DirectionFindingFromModel)
}

// @Summary  Create a manual direction-finding observation
// @Tags     direction-findings
// @ID       createDirectionFinding
// @Accept   json
// @Produce  json
// @Param    body body dto.DirectionFindingRequest true "Direction-finding observation"
// @Success  201 {object} dto.DirectionFindingResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /direction-findings [post]
func (s *Server) createDirectionFinding(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.DirectionFindingRequest](s, w, r, "create direction finding",
		func(ctx context.Context, req dto.DirectionFindingRequest) (configstore.DirectionFindingObservation, error) {
			m := req.ToModel()
			if err := s.store.CreateDirectionFindingObservation(ctx, &m); err != nil {
				return configstore.DirectionFindingObservation{}, err
			}
			return m, nil
		}, dto.DirectionFindingFromModel)
}

// @Summary  Get a manual direction-finding observation
// @Tags     direction-findings
// @ID       getDirectionFinding
// @Produce  json
// @Param    id path int true "Observation id"
// @Success  200 {object} dto.DirectionFindingResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /direction-findings/{id} [get]
func (s *Server) getDirectionFinding(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.DirectionFindingObservation](s, w, r, "get direction finding", id,
		s.store.GetDirectionFindingObservation,
		func(observation *configstore.DirectionFindingObservation) dto.DirectionFindingResponse {
			return dto.DirectionFindingFromModel(*observation)
		})
}

// @Summary  Replace a manual direction-finding observation
// @Tags     direction-findings
// @ID       updateDirectionFinding
// @Accept   json
// @Produce  json
// @Param    id path int true "Observation id"
// @Param    body body dto.DirectionFindingRequest true "Direction-finding observation"
// @Success  200 {object} dto.DirectionFindingResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /direction-findings/{id} [put]
func (s *Server) updateDirectionFinding(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleUpdate[dto.DirectionFindingRequest](s, w, r, "update direction finding", id,
		func(ctx context.Context, id uint32, req dto.DirectionFindingRequest) (configstore.DirectionFindingObservation, error) {
			m := req.ToUpdate(id)
			if err := s.store.UpdateDirectionFindingObservation(ctx, &m); err != nil {
				return configstore.DirectionFindingObservation{}, err
			}
			return m, nil
		}, dto.DirectionFindingFromModel)
}

// @Summary  Delete a manual direction-finding observation
// @Tags     direction-findings
// @ID       deleteDirectionFinding
// @Param    id path int true "Observation id"
// @Success  204 "No Content"
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /direction-findings/{id} [delete]
func (s *Server) deleteDirectionFinding(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleDelete(s, w, r, "delete direction finding", id, s.store.DeleteDirectionFindingObservation)
}
