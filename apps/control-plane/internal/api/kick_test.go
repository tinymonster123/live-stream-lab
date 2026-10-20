package api

import (
	"testing"
)

func TestKickEndpoint(t *testing.T) {
	rooms := newTestRooms()
	mux := NewMux(newTestBroadcasts(), rooms)
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
