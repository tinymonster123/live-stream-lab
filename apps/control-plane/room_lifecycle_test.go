package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fixedClock pins the RoomSession to at. The closure captures a parameter (a
// copy), never a mutable variable, so room actors can read it race-free.
func fixedClock(service *RoomSession, at time.Time) {
	service.now.Set(func() time.Time { return at })
}

// sweepAt moves the clock and runs one lifecycle sweep synchronously, the way
// the room actor's ticker would.
func sweepAt(service *RoomSession, at time.Time) {
	fixedClock(service, at)
	service.sweep(at)
}

func rosterEntry(t *testing.T, service *RoomSession, code, token, participantID string) RosterEntry {
	t.Helper()

	status, err := service.Status(code, token)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, entry := range status.Participants {
		if entry.ParticipantId == participantID {
			return entry
		}
	}
	t.Fatalf("participant %q is missing from the Roster", participantID)
	return RosterEntry{}
}

func TestParticipantGoesOfflineWithoutHeartbeat(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	if !rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("participant is offline right after joining")
	}

	// One second short of the timeout is still online.
	sweepAt(service, base.Add(participantTimeout-time.Second))
	if !rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatalf("participant went offline before %s", participantTimeout)
	}

	sweepAt(service, base.Add(participantTimeout))
	if rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatalf("participant is still online after %s without a heartbeat", participantTimeout)
	}
}

func TestHeartbeatKeepsParticipantOnline(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	// Without a heartbeat the Participant goes offline.
	sweepAt(service, base.Add(2*participantTimeout))
	if rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("participant is still online without a heartbeat")
	}

	// The next heartbeat restores presence without changing the identity.
	at := base.Add(2 * participantTimeout)
	fixedClock(service, at)
	if err := service.Heartbeat(meeting.RoomCode, host.ParticipantId, host.JoinToken); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("heartbeat did not restore presence")
	}

	// Presence then expires relative to the heartbeat, not the original join.
	sweepAt(service, at.Add(participantTimeout-time.Second))
	if !rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("participant went offline before the timeout elapsed since the heartbeat")
	}
	sweepAt(service, at.Add(participantTimeout))
	if rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("participant is still online after the heartbeat timeout")
	}
}

func TestHeartbeatRequiresTheParticipantToken(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.Heartbeat(meeting.RoomCode, guest.ParticipantId, host.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if err := service.Heartbeat(meeting.RoomCode, guest.ParticipantId, meeting.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host token err = %v, want ErrForbidden", err)
	}
	if err := service.Heartbeat("ZZZZZZ", guest.ParticipantId, guest.JoinToken); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("unknown room err = %v, want ErrRoomNotFound", err)
	}
}

func TestOfflineParticipantCanRejoinWithSameIdentity(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	sweepAt(service, base.Add(2*participantTimeout))
	if rosterEntry(t, service, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("participant is still online without a heartbeat")
	}

	rejoined, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Alice",
		ParticipantId: &host.ParticipantId,
	}, host.JoinToken)
	if err != nil {
		t.Fatalf("rejoin after a drop: %v", err)
	}
	if rejoined.ParticipantId != host.ParticipantId {
		t.Fatalf("participantId = %q, want %q", rejoined.ParticipantId, host.ParticipantId)
	}
	if !rosterEntry(t, service, meeting.RoomCode, rejoined.JoinToken, host.ParticipantId).Online {
		t.Fatal("rejoined participant is offline")
	}
}

// A Room whose Participants are all offline is empty: a locked phone keeps the
// Roster entry but does not hold the Room open past the grace period.
func TestEmptyRoomClosesAfterGracePeriod(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	// No heartbeat: the Participant goes offline after the timeout, which
	// starts the empty-room grace period.
	offlineAt := base.Add(participantTimeout)
	sweepAt(service, offlineAt)

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Closing {
		t.Fatalf("state = %q, want closing while every Participant is offline", status.State)
	}

	// One second before the grace period ends the Room is still open.
	sweepAt(service, offlineAt.Add(emptyRoomGrace-time.Second))
	status, err = service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Closing {
		t.Fatalf("state = %q, want closing %s after going offline", status.State, emptyRoomGrace-time.Second)
	}

	sweepAt(service, offlineAt.Add(emptyRoomGrace))
	status, err = service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Closed {
		t.Fatalf("state = %q, want closed %s after going offline", status.State, emptyRoomGrace)
	}

	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{DisplayName: "Bob"}, ""); !errors.Is(err, ErrRoomClosed) {
		t.Fatalf("join after close: err = %v, want ErrRoomClosed", err)
	}
}

func TestRoomClosesAfterTheLastParticipantLeaves(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	leftAt := base.Add(5 * time.Second)
	fixedClock(service, leftAt)
	if err := service.Leave(meeting.RoomCode, host.ParticipantId, host.JoinToken); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// An empty Roster has no member left to read status; the close is observed
	// through the join that comes too late.
	sweepAt(service, leftAt.Add(emptyRoomGrace))
	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{DisplayName: "Bob"}, ""); !errors.Is(err, ErrRoomClosed) {
		t.Fatalf("join after close: err = %v, want ErrRoomClosed", err)
	}
}

func TestJoinDuringTheEmptyGracePeriodReopensTheRoom(t *testing.T) {
	service := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(service, base)
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	leftAt := base.Add(5 * time.Second)
	fixedClock(service, leftAt)
	if err := service.Leave(meeting.RoomCode, host.ParticipantId, host.JoinToken); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	sweepAt(service, leftAt.Add(emptyRoomGrace/2))
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	status, err := service.Status(meeting.RoomCode, guest.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Open {
		t.Fatalf("state = %q, want open after a join", status.State)
	}

	// The reopened Room outlives the original grace period as long as the
	// Participant keeps heartbeating.
	for _, at := range []time.Time{
		leftAt.Add(emptyRoomGrace),
		leftAt.Add(2 * emptyRoomGrace),
	} {
		fixedClock(service, at)
		if err := service.Heartbeat(meeting.RoomCode, guest.ParticipantId, guest.JoinToken); err != nil {
			t.Fatalf("Heartbeat at %s: %v", at.Sub(base), err)
		}
		sweepAt(service, at)
		status, err = service.Status(meeting.RoomCode, guest.JoinToken)
		if err != nil {
			t.Fatalf("Status at %s: %v", at.Sub(base), err)
		}
		if status.State != Open {
			t.Fatalf("state = %q at %s, want open while a Participant is online", status.State, at.Sub(base))
		}
	}
}

func TestKickRemovesParticipantAndBlocksRejoin(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.Kick(meeting.RoomCode, RoomKickRequest{
		ParticipantId: guest.ParticipantId,
	}, host.JoinToken); err != nil {
		t.Fatalf("Kick: %v", err)
	}

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Participants) != 1 || status.Participants[0].ParticipantId != host.ParticipantId {
		t.Fatalf("participants = %+v, want only the host", status.Participants)
	}

	// The kicked identity cannot rejoin, with or without an explicit id.
	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Bob",
		ParticipantId: &guest.ParticipantId,
	}, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("rejoin with participantId: err = %v, want ErrForbidden", err)
	}
	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName: "Bob",
	}, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("rejoin with token only: err = %v, want ErrForbidden", err)
	}

	// The kicked Participant's token grants nothing: not status, publishing,
	// or a subscription.
	if _, err := service.Status(meeting.RoomCode, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("status after kick: err = %v, want ErrForbidden", err)
	}
	if err := service.UpdateTracks(meeting.RoomCode, RoomTrackUpdate{
		ParticipantId: guest.ParticipantId,
	}, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("tracks after kick: err = %v, want ErrForbidden", err)
	}
	if _, err := service.Subscribe(meeting.RoomCode, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("subscribe after kick: err = %v, want ErrForbidden", err)
	}

	// A different Participant can still join.
	if other := joinRoom(t, service, meeting.RoomCode, "Carol", ""); other.ParticipantId == guest.ParticipantId {
		t.Fatal("a new Participant reused the kicked participantId")
	}
}

func TestKickRequiresTheHost(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.Kick(meeting.RoomCode, RoomKickRequest{
		ParticipantId: host.ParticipantId,
	}, guest.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("guest kick: err = %v, want ErrForbidden", err)
	}
	if err := service.Kick(meeting.RoomCode, RoomKickRequest{
		ParticipantId: guest.ParticipantId,
	}, meeting.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("host token kick: err = %v, want ErrForbidden", err)
	}
	if err := service.Kick(meeting.RoomCode, RoomKickRequest{
		ParticipantId: host.ParticipantId,
	}, host.JoinToken); !errors.Is(err, ErrInvalidRoomRequest) {
		t.Fatalf("self kick: err = %v, want ErrInvalidRoomRequest", err)
	}
	if err := service.Kick("ZZZZZZ", RoomKickRequest{
		ParticipantId: guest.ParticipantId,
	}, host.JoinToken); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("unknown room: err = %v, want ErrRoomNotFound", err)
	}

	// The failed attempts left the Roster intact.
	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Participants) != 2 {
		t.Fatalf("participants = %+v, want both", status.Participants)
	}
}

func TestKickEndpoint(t *testing.T) {
	rooms := newTestRoomSession()
	mux := newMux(newTestService(&fakeRunner{}), rooms)
	meeting := createMeeting(t, rooms, "Alice")
	host := joinRoom(t, rooms, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, rooms, meeting.RoomCode, "Bob", "")
	path := "/api/v1/rooms/" + meeting.RoomCode + "/kick"
	body := `{"participantId":"` + guest.ParticipantId + `"}`

	if recorder := performAuthorizedRequest(t, mux, "POST", path, body, guest.JoinToken); recorder.Code != 403 {
		t.Fatalf("guest kick status = %d, want 403 (body %s)", recorder.Code, recorder.Body.String())
	}
	if recorder := performAuthorizedRequest(t, mux, "POST", path, `{`, host.JoinToken); recorder.Code != 400 {
		t.Fatalf("malformed body status = %d, want 400", recorder.Code)
	}
	if recorder := performAuthorizedRequest(t, mux, "POST", path, body, host.JoinToken); recorder.Code != 204 {
		t.Fatalf("host kick status = %d, want 204 (body %s)", recorder.Code, recorder.Body.String())
	}
	// A retried kick of an absent Participant is idempotent.
	if recorder := performAuthorizedRequest(t, mux, "POST", path, body, host.JoinToken); recorder.Code != 204 {
		t.Fatalf("retried kick status = %d, want 204", recorder.Code)
	}
}

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
	rooms := newTestRoomSession()
	base := time.Unix(1_700_000_000, 0)
	fixedClock(rooms, base)
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

	// The host is the only Participant, so the first message is its own
	// presence change.
	sweepAt(rooms, base.Add(2*participantTimeout))
	if eventType, participantID := readPresenceEvent(t, conn); eventType != "participant.offline" || participantID != host.ParticipantId {
		t.Fatalf("presence event = %s for %s, want participant.offline", eventType, participantID)
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"heartbeat"}`)); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}
	if eventType, participantID := readPresenceEvent(t, conn); eventType != "participant.online" || participantID != host.ParticipantId {
		t.Fatalf("presence event = %s for %s, want participant.online", eventType, participantID)
	}

	if !rosterEntry(t, rooms, meeting.RoomCode, host.JoinToken, host.ParticipantId).Online {
		t.Fatal("heartbeat over the signaling socket did not restore presence")
	}
}

func TestWebSocketClosesForKickedParticipant(t *testing.T) {
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

	if err := rooms.Kick(meeting.RoomCode, RoomKickRequest{
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
