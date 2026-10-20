package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func roomSocketURL(serverURL, roomCode, token string) string {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		panic(err)
	}
	parsed.Scheme = "ws"
	parsed.Path = "/api/v1/rooms/" + roomCode + "/ws"
	query := parsed.Query()
	query.Set("joinToken", token)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func readRawEvent(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	return data
}

func readJoinedEvent(t *testing.T, conn *websocket.Conn) ParticipantJoinedEvent {
	t.Helper()

	var event ParticipantJoinedEvent
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode joined event: %v", err)
	}
	return event
}

func readLeftEvent(t *testing.T, conn *websocket.Conn) ParticipantLeftEvent {
	t.Helper()

	var event ParticipantLeftEvent
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode left event: %v", err)
	}
	return event
}

func TestWebSocketRejectsUnusableTokens(t *testing.T) {
	rooms := newTestRoomSession()
	server := httptest.NewServer(newMux(newTestService(&fakeRunner{}), rooms))
	defer server.Close()
	meeting := createMeeting(t, rooms, "Alice")

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "invalid token", token: "not-a-token"},
		{name: "host token before join", token: meeting.JoinToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, response, err := websocket.Dial(
				ctx,
				roomSocketURL(server.URL, meeting.RoomCode, test.token),
				nil,
			)
			if conn != nil {
				_ = conn.Close(websocket.StatusNormalClosure, "")
			}
			if err == nil {
				t.Fatal("Dial succeeded, want a rejected handshake")
			}
			if response == nil || response.StatusCode != http.StatusForbidden {
				t.Fatalf("response = %v, want HTTP 403", response)
			}
		})
	}
}

func TestWebSocketSendsExistingRosterToNewSubscriber(t *testing.T) {
	rooms := newTestRoomSession()
	server := httptest.NewServer(newMux(newTestService(&fakeRunner{}), rooms))
	defer server.Close()

	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")

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

	snapshot := readJoinedEvent(t, conn)
	if snapshot.ParticipantId != guest.ParticipantId || snapshot.DisplayName != "Bob" {
		t.Fatalf("snapshot event = %+v, want the existing guest", snapshot)
	}
}

func TestWebSocketBroadcastsJoinLeaveAndRejoin(t *testing.T) {
	rooms := newTestRoomSession()
	server := httptest.NewServer(newMux(newTestService(&fakeRunner{}), rooms))
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

	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")
	joined := readJoinedEvent(t, conn)
	if joined.Type != "participant.joined" {
		t.Fatalf("type = %v, want participant.joined", joined.Type)
	}
	if joined.ParticipantId != guest.ParticipantId || joined.DisplayName != "Bob" {
		t.Fatalf("joined event = %+v", joined)
	}
	if joined.Role != Guest {
		t.Fatalf("role = %q, want guest", joined.Role)
	}

	rejoined, err := rooms.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Robert",
		ParticipantId: &guest.ParticipantId,
	}, guest.JoinToken)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if rejoined.ParticipantId != guest.ParticipantId {
		t.Fatalf("rejoin id = %q, want %q", rejoined.ParticipantId, guest.ParticipantId)
	}
	renamed := readJoinedEvent(t, conn)
	if renamed.ParticipantId != guest.ParticipantId || renamed.DisplayName != "Robert" {
		t.Fatalf("rejoin event = %+v", renamed)
	}

	if err := rooms.Leave(meeting.RoomCode, guest.ParticipantId, guest.JoinToken); err != nil {
		t.Fatalf("leave: %v", err)
	}
	left := readLeftEvent(t, conn)
	if left.Type != "participant.left" || left.ParticipantId != guest.ParticipantId {
		t.Fatalf("left event = %+v", left)
	}
}
