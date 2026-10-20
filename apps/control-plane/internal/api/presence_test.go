package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

func readPresenceEvent(t *testing.T, conn *websocket.Conn) (string, string) {
	t.Helper()

	var event struct {
		Type          string `json:"type"`
		ParticipantId string `json:"participantId"`
	}
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode presence event: %v", err)
	}
	return event.Type, event.ParticipantId
}

func TestWebSocketHeartbeatRestoresPresence(t *testing.T) {
	// A short heartbeat timeout keeps the real sweep ticker in the loop while
	// letting the test observe the offline transition in about a second.
	rooms := newTestRoomsWithHeartbeatTimeout(50 * time.Millisecond)
	server := httptest.NewServer(NewMux(newTestBroadcasts(), rooms))
	defer server.Close()

	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(
		ctx,
		roomSocketURL(server.URL, meeting.RoomCode, host.JoinToken),
		nil,
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The host is the only Participant, so the next message after the sweep
	// ticker fires is its own presence change.
	if eventType, participantID := readPresenceEvent(t, conn); eventType != "participant.offline" || participantID != host.ParticipantId {
		t.Fatalf("presence event = %s for %s, want participant.offline", eventType, participantID)
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"heartbeat"}`)); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	if eventType, participantID := readPresenceEvent(t, conn); eventType != "participant.online" || participantID != host.ParticipantId {
		t.Fatalf("presence event = %s for %s, want participant.online", eventType, participantID)
	}
}

func TestWebSocketClosesForKickedParticipant(t *testing.T) {
	rooms := newTestRooms()
	server := httptest.NewServer(NewMux(newTestBroadcasts(), rooms))
	defer server.Close()

	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(
		ctx,
		roomSocketURL(server.URL, meeting.RoomCode, guest.JoinToken),
		nil,
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The Roster replay carries the host.
	if joined := readJoinedEvent(t, conn); joined.ParticipantId != host.ParticipantId {
		t.Fatalf("roster replay = %+v, want the host", joined)
	}

	if err := rooms.Kick(meeting.RoomCode, contracts.RoomKickRequest{
		ParticipantId: guest.ParticipantId,
	}, host.JoinToken); err != nil {
		t.Fatalf("Kick: %v", err)
	}

	_, _, err = conn.Read(ctx)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read after kick: err = %v, want a close error", err)
	}
	if closeErr.Code != websocket.StatusPolicyViolation || closeErr.Reason != kickCloseReason {
		t.Fatalf("close = %d %q, want %d %q", closeErr.Code, closeErr.Reason, websocket.StatusPolicyViolation, kickCloseReason)
	}
}
