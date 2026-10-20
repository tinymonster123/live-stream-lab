package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const socketWriteTimeout = 5 * time.Second

func (service *RoomSession) handleCreateMeeting(writer http.ResponseWriter, request *http.Request) {
	var body MeetingCreateRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	meeting, err := service.Create(body)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, meeting)
}

func (service *RoomSession) handleJoinRoom(writer http.ResponseWriter, request *http.Request) {
	var body RoomJoinRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	grant, err := service.Join(
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

func (service *RoomSession) handleUpdateRoomTracks(writer http.ResponseWriter, request *http.Request) {
	var body RoomTrackUpdate
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := service.UpdateTracks(
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

func (service *RoomSession) handleUpdateRoomMute(writer http.ResponseWriter, request *http.Request) {
	var body RoomMuteUpdate
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := service.UpdateMute(
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

func (service *RoomSession) handleLeaveRoom(writer http.ResponseWriter, request *http.Request) {
	var body RoomLeaveRequest
	if err := decodeJSON(request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	err := service.Leave(
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

func (service *RoomSession) handleRoomStatus(writer http.ResponseWriter, request *http.Request) {
	status, err := service.Status(
		request.PathValue("roomCode"),
		bearerToken(request),
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (service *RoomSession) handleRoomSocket(writer http.ResponseWriter, request *http.Request) {
	roomCode := request.PathValue("roomCode")
	subscriber, err := service.Subscribe(roomCode, request.URL.Query().Get("joinToken"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	defer service.Unsubscribe(roomCode, subscriber)

	// The local lab has no separate auth origin; allow the Vite dev origin.
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("room %s: websocket accept: %v", roomCode, err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := conn.CloseRead(request.Context())
	for {
		select {
		case <-ctx.Done():
			return
		case <-subscriber.done:
			return
		case event, ok := <-subscriber.events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				log.Printf("room %s: encode event: %v", roomCode, err)
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, socketWriteTimeout)
			err = conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				log.Printf("room %s: write event: %v", roomCode, err)
				return
			}
		}
	}
}

func bearerToken(request *http.Request) string {
	fields := strings.Fields(request.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}
