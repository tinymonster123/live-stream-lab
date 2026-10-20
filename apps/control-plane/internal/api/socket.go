package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

const (
	socketWriteTimeout = 5 * time.Second
	socketReadLimit    = 1024
	kickCloseReason    = string(contracts.Kicked)
)

// clientMessage is the only message a Participant sends over the signaling
// socket. SDP and media never travel here.
type clientMessage struct {
	Type string `json:"type"`
}

func (server *Server) roomSocket(writer http.ResponseWriter, request *http.Request) {
	roomCode := request.PathValue("roomCode")
	joinToken := request.URL.Query().Get("joinToken")
	subscriber, err := server.rooms.Subscribe(roomCode, joinToken)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	defer server.rooms.Unsubscribe(roomCode, subscriber)

	// The local lab has no separate auth origin; allow the Vite dev origin.
	conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("room %s: websocket accept: %v", roomCode, err)
		return
	}

	ctx, cancel := context.WithCancel(request.Context())
	closeStatus := websocket.StatusNormalClosure
	closeReason := ""
	defer func() {
		// Write the close frame before canceling the read context: canceling
		// it closes the underlying connection and would race the frame.
		_ = conn.Close(closeStatus, closeReason)
		cancel()
	}()

	conn.SetReadLimit(socketReadLimit)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var message clientMessage
			if err := json.Unmarshal(data, &message); err != nil || message.Type != "heartbeat" {
				continue
			}
			// The handshake already proved the token; Heartbeat verifies it
			// again so presence follows the same authorization path as the
			// other operations.
			if err := server.rooms.Heartbeat(roomCode, subscriber.ParticipantID(), joinToken); err != nil {
				log.Printf("room %s: heartbeat from %s: %v", roomCode, subscriber.ParticipantID(), err)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readDone:
			return
		case <-subscriber.Done():
			// The Room removed this Participant. A kick names itself so the
			// client can tell it apart from a network drop.
			if subscriber.CloseReason() == kickCloseReason {
				closeStatus = websocket.StatusPolicyViolation
				closeReason = kickCloseReason
			}
			return
		case event, ok := <-subscriber.Events():
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
