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

func readTrackPublishedEvent(t *testing.T, conn *websocket.Conn) TrackPublishedEvent {
	t.Helper()

	var event TrackPublishedEvent
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode track.published event: %v", err)
	}
	return event
}

func readTrackUnpublishedEvent(t *testing.T, conn *websocket.Conn) TrackUnpublishedEvent {
	t.Helper()

	var event TrackUnpublishedEvent
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode track.unpublished event: %v", err)
	}
	return event
}

func TestWebSocketBroadcastsTrackPublishAndUnpublish(t *testing.T) {
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

	// The subscriber first receives the current Roster, including the guest.
	if replay := readJoinedEvent(t, conn); replay.ParticipantId != guest.ParticipantId {
		t.Fatalf("roster replay = %+v, want the guest", replay)
	}

	publishTracks(t, rooms, meeting.RoomCode, guest.ParticipantId, guest.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
	)
	published := readTrackPublishedEvent(t, conn)
	if published.Type != "track.published" {
		t.Fatalf("type = %v, want track.published", published.Type)
	}
	if published.ParticipantId != guest.ParticipantId || published.TrackId != "camera" {
		t.Fatalf("event = %+v", published)
	}
	if published.Kind != TrackPublishedEventKindVideo {
		t.Fatalf("kind = %q, want video", published.Kind)
	}
	if want := "/rtc/v1/whep/?app=live&stream=" + guest.ParticipantId; published.WhepUrl != want {
		t.Fatalf("whepUrl = %q, want %q", published.WhepUrl, want)
	}

	publishTracks(t, rooms, meeting.RoomCode, guest.ParticipantId, guest.JoinToken)
	unpublished := readTrackUnpublishedEvent(t, conn)
	if unpublished.ParticipantId != guest.ParticipantId || unpublished.TrackId != "camera" {
		t.Fatalf("event = %+v", unpublished)
	}
}

func TestWebSocketReplaysPublishedTracksToNewSubscriber(t *testing.T) {
	rooms := newTestRoomSession()
	server := httptest.NewServer(newMux(newTestService(&fakeRunner{}), rooms))
	defer server.Close()

	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")
	publishTracks(t, rooms, meeting.RoomCode, guest.ParticipantId, guest.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
	)

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

	joined := readJoinedEvent(t, conn)
	if joined.ParticipantId != guest.ParticipantId {
		t.Fatalf("joined replay = %+v, want the guest", joined)
	}
	published := readTrackPublishedEvent(t, conn)
	if published.ParticipantId != guest.ParticipantId || published.TrackId != "camera" {
		t.Fatalf("track replay = %+v", published)
	}
}

func readMuteChangedEvent(t *testing.T, conn *websocket.Conn) MuteChangedEvent {
	t.Helper()

	var event MuteChangedEvent
	if err := json.Unmarshal(readRawEvent(t, conn), &event); err != nil {
		t.Fatalf("decode mute.changed event: %v", err)
	}
	return event
}

func TestWebSocketBroadcastsMuteChanges(t *testing.T) {
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

	if replay := readJoinedEvent(t, conn); replay.ParticipantId != guest.ParticipantId {
		t.Fatalf("roster replay = %+v, want the guest", replay)
	}

	if err := rooms.UpdateMute(meeting.RoomCode, RoomMuteUpdate{
		ParticipantId: guest.ParticipantId,
		Muted:         true,
	}, guest.JoinToken); err != nil {
		t.Fatalf("mute: %v", err)
	}
	muted := readMuteChangedEvent(t, conn)
	if muted.ParticipantId != guest.ParticipantId || !muted.Muted {
		t.Fatalf("mute event = %+v", muted)
	}

	if err := rooms.UpdateMute(meeting.RoomCode, RoomMuteUpdate{
		ParticipantId: guest.ParticipantId,
		Muted:         false,
	}, guest.JoinToken); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	unmuted := readMuteChangedEvent(t, conn)
	if unmuted.ParticipantId != guest.ParticipantId || unmuted.Muted {
		t.Fatalf("unmute event = %+v", unmuted)
	}
}

func TestWebSocketReplaysMuteStateToNewSubscriber(t *testing.T) {
	rooms := newTestRoomSession()
	server := httptest.NewServer(newMux(newTestService(&fakeRunner{}), rooms))
	defer server.Close()

	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")
	if err := rooms.UpdateMute(meeting.RoomCode, RoomMuteUpdate{
		ParticipantId: guest.ParticipantId,
		Muted:         true,
	}, guest.JoinToken); err != nil {
		t.Fatalf("mute: %v", err)
	}

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

	if joined := readJoinedEvent(t, conn); joined.ParticipantId != guest.ParticipantId {
		t.Fatalf("joined replay = %+v, want the guest", joined)
	}
	muted := readMuteChangedEvent(t, conn)
	if muted.ParticipantId != guest.ParticipantId || !muted.Muted {
		t.Fatalf("mute replay = %+v", muted)
	}
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
