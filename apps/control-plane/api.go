package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

const maxRequestBytes = 1 << 20

func newMux(broadcasts *BroadcastService, rooms *RoomSession) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("POST /api/v1/broadcasts", broadcasts.handleCreateBroadcast)
	mux.HandleFunc("GET /api/v1/broadcasts/{broadcastId}", broadcasts.handleBroadcastStatus)
	mux.HandleFunc("POST /api/v1/broadcasts/{broadcastId}/start", broadcasts.handleStartBroadcast)
	mux.HandleFunc("POST /api/v1/broadcasts/{broadcastId}/stop", broadcasts.handleStopBroadcast)

	mux.HandleFunc("POST /api/v1/meetings", rooms.handleCreateMeeting)
	mux.HandleFunc("GET /api/v1/rooms/{roomCode}", rooms.handleRoomStatus)
	mux.HandleFunc("POST /api/v1/rooms/{roomCode}/join", rooms.handleJoinRoom)
	mux.HandleFunc("POST /api/v1/rooms/{roomCode}/leave", rooms.handleLeaveRoom)
	mux.HandleFunc("GET /api/v1/rooms/{roomCode}/ws", rooms.handleRoomSocket)
	return mux
}

func handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, HealthResponse{Status: Ok, Service: ControlPlane})
}

func (service *BroadcastService) handleCreateBroadcast(writer http.ResponseWriter, request *http.Request) {
	var spec BroadcastCreateRequest
	if err := decodeJSON(request, &spec); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	session, err := service.Create(spec)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, session)
}

func (service *BroadcastService) handleBroadcastStatus(writer http.ResponseWriter, request *http.Request) {
	session, err := service.Status(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (service *BroadcastService) handleStartBroadcast(writer http.ResponseWriter, request *http.Request) {
	session, err := service.Start(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (service *BroadcastService) handleStopBroadcast(writer http.ResponseWriter, request *http.Request) {
	session, err := service.Stop(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, Error{Code: code, Message: message})
}

func writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrRoomNotFound):
		writeError(writer, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrForbidden),
		errors.Is(err, ErrInvalidJoinToken),
		errors.Is(err, ErrExpiredJoinToken),
		errors.Is(err, ErrRoomClosed):
		writeError(writer, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, ErrInvalidBroadcast),
		errors.Is(err, ErrUnsupportedLatency),
		errors.Is(err, ErrInvalidRoomRequest):
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		log.Printf("request failed: %v", err)
		writeError(writer, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
