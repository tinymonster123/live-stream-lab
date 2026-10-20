// Package api is the HTTP and WebSocket transport for the control plane. It
// translates requests into domain calls and maps domain errors to statuses.
package api

import (
	"net/http"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/broadcast"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/room"
)

// Server holds the domain services the routes call.
type Server struct {
	broadcasts *broadcast.Service
	rooms      *room.Session
}

// NewMux wires the control-plane routes.
func NewMux(broadcasts *broadcast.Service, rooms *room.Session) *http.ServeMux {
	server := &Server{broadcasts: broadcasts, rooms: rooms}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)

	mux.HandleFunc("POST /api/v1/broadcasts", server.createBroadcast)
	mux.HandleFunc("GET /api/v1/broadcasts/{broadcastId}", server.broadcastStatus)
	mux.HandleFunc("POST /api/v1/broadcasts/{broadcastId}/start", server.startBroadcast)
	mux.HandleFunc("POST /api/v1/broadcasts/{broadcastId}/stop", server.stopBroadcast)

	mux.HandleFunc("POST /api/v1/meetings", server.createMeeting)
	mux.HandleFunc("GET /api/v1/rooms/{roomCode}", server.roomStatus)
	mux.HandleFunc("POST /api/v1/rooms/{roomCode}/join", server.joinRoom)
	mux.HandleFunc("POST /api/v1/rooms/{roomCode}/leave", server.leaveRoom)
	mux.HandleFunc("POST /api/v1/rooms/{roomCode}/kick", server.kickParticipant)
	mux.HandleFunc("PUT /api/v1/rooms/{roomCode}/tracks", server.updateRoomTracks)
	mux.HandleFunc("PUT /api/v1/rooms/{roomCode}/mute", server.updateRoomMute)
	mux.HandleFunc("GET /api/v1/rooms/{roomCode}/ws", server.roomSocket)
	return mux
}

func handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, contracts.HealthResponse{
		Status:  contracts.Ok,
		Service: contracts.ControlPlane,
	})
}
