package api

import (
	"net/http"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

func (server *Server) createBroadcast(writer http.ResponseWriter, request *http.Request) {
	var spec contracts.BroadcastCreateRequest
	if err := decodeJSON(request, &spec); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	session, err := server.broadcasts.Create(spec)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, session)
}

func (server *Server) broadcastStatus(writer http.ResponseWriter, request *http.Request) {
	session, err := server.broadcasts.Status(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (server *Server) startBroadcast(writer http.ResponseWriter, request *http.Request) {
	session, err := server.broadcasts.Start(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (server *Server) stopBroadcast(writer http.ResponseWriter, request *http.Request) {
	session, err := server.broadcasts.Stop(request.PathValue("broadcastId"))
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}
