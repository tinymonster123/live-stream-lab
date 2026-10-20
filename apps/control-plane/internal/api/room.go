package api

import (
	"net/http"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

func (server *Server) createMeeting(writer http.ResponseWriter, request *http.Request) {
	var body contracts.MeetingCreateRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	meeting, err := server.rooms.Create(body)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, meeting)
}

func (server *Server) joinRoom(writer http.ResponseWriter, request *http.Request) {
	var body contracts.RoomJoinRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	grant, err := server.rooms.Join(
		request.PathValue("roomCode"),
		body,
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, grant)
}

func (server *Server) leaveRoom(writer http.ResponseWriter, request *http.Request) {
	var body contracts.RoomLeaveRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := server.rooms.Leave(
		request.PathValue("roomCode"),
		body.ParticipantId,
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) kickParticipant(writer http.ResponseWriter, request *http.Request) {
	var body contracts.RoomKickRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := server.rooms.Kick(
		request.PathValue("roomCode"),
		body,
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) updateRoomTracks(writer http.ResponseWriter, request *http.Request) {
	var body contracts.RoomTrackUpdate
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := server.rooms.UpdateTracks(
		request.PathValue("roomCode"),
		body,
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) updateRoomMute(writer http.ResponseWriter, request *http.Request) {
	var body contracts.RoomMuteUpdate
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := server.rooms.UpdateMute(
		request.PathValue("roomCode"),
		body,
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) roomStatus(writer http.ResponseWriter, request *http.Request) {
	status, err := server.rooms.Status(
		request.PathValue("roomCode"),
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}
