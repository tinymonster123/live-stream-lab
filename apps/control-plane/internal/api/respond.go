package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/broadcast"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/room"
)

const maxRequestBytes = 1 << 20

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
	writeJSON(writer, status, contracts.Error{Code: code, Message: message})
}

// writeServiceError is the one place domain errors become HTTP statuses, so
// the domain packages never import net/http.
func writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, broadcast.ErrNotFound), errors.Is(err, room.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, room.ErrForbidden),
		errors.Is(err, room.ErrInvalidToken),
		errors.Is(err, room.ErrExpiredToken),
		errors.Is(err, room.ErrClosed):
		writeError(writer, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, broadcast.ErrInvalid),
		errors.Is(err, broadcast.ErrUnsupportedLatency),
		errors.Is(err, room.ErrInvalidRequest):
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		log.Printf("request failed: %v", err)
		writeError(writer, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func bearerToken(request *http.Request) string {
	fields := strings.Fields(request.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return ""
	}
	return fields[1]
}
