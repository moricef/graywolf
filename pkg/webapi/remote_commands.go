package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerRemoteCommands(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote-actions/command-credentials", s.listRemoteCommandCredentials)
	mux.HandleFunc("POST /api/remote-actions/command-credentials", s.createRemoteCommandCredential)
	mux.HandleFunc("PUT /api/remote-actions/command-credentials/{id}", s.updateRemoteCommandCredential)
	mux.HandleFunc("DELETE /api/remote-actions/command-credentials/{id}", s.deleteRemoteCommandCredential)
	mux.HandleFunc("POST /api/remote-actions/commands/{target}", s.sendRemoteCommand)
}

func commandCredentialDTO(row remoteactions.RemoteCommandCredential) dto.RemoteCommandCredential {
	out := dto.RemoteCommandCredential{
		ID:          row.ID,
		Name:        row.Name,
		TargetCall:  row.TargetCall,
		KeyID:       row.KeyID,
		LastCounter: strconv.FormatUint(row.LastCounter, 10),
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.LastUsedAt != nil {
		value := row.LastUsedAt.UTC().Format(time.RFC3339)
		out.LastUsedAt = &value
	}
	return out
}

// listRemoteCommandCredentials returns metadata for every installed !RC1 key.
// Secrets are never present in the response.
//
// @Summary  List authenticated remote-control credentials
// @Tags     remote-actions
// @ID       listRemoteCommandCredentials
// @Produce  json
// @Success  200 {array} dto.RemoteCommandCredential
// @Security CookieAuth
// @Router   /remote-actions/command-credentials [get]
func (s *Server) listRemoteCommandCredentials(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	rows, err := s.remoteActions.Commands().List(r.Context())
	if err != nil {
		s.internalError(w, r, "list remote command credentials", err)
		return
	}
	out := make([]dto.RemoteCommandCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, commandCredentialDTO(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary  Install an authenticated remote-control credential
// @Tags     remote-actions
// @ID       createRemoteCommandCredential
// @Accept   json
// @Produce  json
// @Param    body body dto.RemoteCommandCredentialRequest true "Credential"
// @Success  201 {object} dto.RemoteCommandCredential
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  409 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /remote-actions/command-credentials [post]
func (s *Server) createRemoteCommandCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	var in dto.RemoteCommandCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		badRequest(w, "name required")
		return
	}
	row := &remoteactions.RemoteCommandCredential{
		Name: strings.TrimSpace(in.Name), TargetCall: in.TargetCall, SecretBase64URL: in.SecretBase64URL,
	}
	if err := s.remoteActions.Commands().Create(r.Context(), row); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "a command credential already exists for this target")
			return
		}
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, commandCredentialDTO(*row))
}

// @Summary  Update an authenticated remote-control credential
// @Tags     remote-actions
// @ID       updateRemoteCommandCredential
// @Accept   json
// @Produce  json
// @Param    id path int true "Credential id"
// @Param    body body dto.RemoteCommandCredentialRequest true "Credential"
// @Success  200 {object} dto.RemoteCommandCredential
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /remote-actions/command-credentials/{id} [put]
func (s *Server) updateRemoteCommandCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	row, err := s.remoteActions.Commands().Get(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get remote command credential", err)
		return
	}
	var in dto.RemoteCommandCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	if strings.TrimSpace(in.Name) != "" {
		row.Name = strings.TrimSpace(in.Name)
	}
	if strings.TrimSpace(in.TargetCall) != "" {
		row.TargetCall = in.TargetCall
	}
	replaceSecret := strings.TrimSpace(in.SecretBase64URL) != ""
	if replaceSecret {
		row.SecretBase64URL = in.SecretBase64URL
	}
	if err := s.remoteActions.Commands().Update(r.Context(), row, replaceSecret); err != nil {
		if isUniqueConstraintErr(err) {
			conflict(w, "a command credential already exists for this target")
			return
		}
		badRequest(w, err.Error())
		return
	}
	updated, err := s.remoteActions.Commands().Get(r.Context(), row.ID)
	if err != nil {
		s.internalError(w, r, "reload remote command credential", err)
		return
	}
	writeJSON(w, http.StatusOK, commandCredentialDTO(*updated))
}

// @Summary  Remove an authenticated remote-control credential
// @Tags     remote-actions
// @ID       deleteRemoteCommandCredential
// @Param    id path int true "Credential id"
// @Success  204
// @Security CookieAuth
// @Router   /remote-actions/command-credentials/{id} [delete]
func (s *Server) deleteRemoteCommandCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if err := s.remoteActions.Commands().Delete(r.Context(), uint(id)); err != nil {
		s.internalError(w, r, "delete remote command credential", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendRemoteCommand reserves a durable counter, builds the CA2RXU !RC1 HMAC
// envelope and submits it through the normal APRS Messages transport.
//
// @Summary  Queue an authenticated remote command
// @Tags     remote-actions
// @ID       sendRemoteCommand
// @Accept   json
// @Produce  json
// @Param    target path string true "Target callsign"
// @Param    body body dto.RemoteCommandSendRequest true "Command"
// @Success  202 {object} dto.RemoteCommandSendResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /remote-actions/commands/{target} [post]
func (s *Server) sendRemoteCommand(w http.ResponseWriter, r *http.Request) {
	if !s.requireRemoteActions(w) {
		return
	}
	svc, ok := s.requireMessagesSvc(w)
	if !ok {
		return
	}
	var in dto.RemoteCommandSendRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid JSON")
		return
	}
	target, err := remoteactions.NormalizeTargetCall(r.PathValue("target"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	command, err := remoteactions.NormalizeRemoteCommand(in.Command)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	controller, err := s.resolveOurCall(r.Context())
	if err != nil {
		s.internalError(w, r, "resolve controller callsign", err)
		return
	}
	controller = strings.ToUpper(strings.TrimSpace(controller))
	if controller == "" || strings.HasPrefix(controller, "N0CALL") {
		badRequest(w, "station callsign is not configured")
		return
	}
	if target == controller {
		badRequest(w, "cannot send a command to our own callsign")
		return
	}
	if in.Channel != nil && *in.Channel != 0 {
		mode, err := s.store.ModeForChannel(r.Context(), *in.Channel)
		if err != nil {
			s.internalError(w, r, "mode-for-channel lookup", err)
			return
		}
		if mode == configstore.ChannelModePacket {
			badRequest(w, "channel is packet-mode; choose an aprs or aprs+packet channel")
			return
		}
	}

	envelope, counter, err := s.remoteActions.Commands().ReserveEnvelope(
		r.Context(), target, controller, command,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "reserve remote command counter", err)
		return
	}
	svcReq := messages.SendMessageRequest{
		To: target, Text: envelope, OurCall: controller, ThreadKind: messages.ThreadKindDM,
	}
	if in.Channel != nil {
		svcReq.Channel = *in.Channel
	}
	row, err := svc.SendMessage(r.Context(), svcReq)
	if err != nil {
		// The reserved counter remains consumed deliberately. Reusing it after
		// an ambiguous send failure would violate the anti-replay contract.
		s.internalError(w, r, "send authenticated remote command", err)
		return
	}
	writeJSON(w, http.StatusAccepted, dto.RemoteCommandSendResponse{
		Counter: strings.ToUpper(strconv.FormatUint(counter, 36)), Message: dto.MessageFromModel(*row),
	})
}
