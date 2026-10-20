package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testRoomCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newTestRoomSession() *RoomSession {
	service, err := NewRoomSession(RoomConfig{
		JoinSecret: []byte("test-secret"),
		WhipBase:   "http://127.0.0.1:8080/rtc/v1/whip/",
		TTL:        time.Hour,
	})
	if err != nil {
		panic(err)
	}
	return service
}

func createMeeting(t *testing.T, service *RoomSession, host string) MeetingCreated {
	t.Helper()

	meeting, err := service.Create(MeetingCreateRequest{HostDisplayName: host})
	if err != nil {
		t.Fatalf("Create meeting: %v", err)
	}
	return meeting
}

func joinRoom(t *testing.T, service *RoomSession, code, name, bearer string) JoinGrant {
	t.Helper()

	request := RoomJoinRequest{DisplayName: name}
	grant, err := service.Join(code, request, bearer)
	if err != nil {
		t.Fatalf("Join %q: %v", name, err)
	}
	return grant
}

func TestCreateMeetingReturnsRoomCodeAndHostToken(t *testing.T) {
	service := newTestRoomSession()
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }

	meeting := createMeeting(t, service, "Alice")

	if len(meeting.RoomCode) != roomCodeLength {
		t.Fatalf("roomCode = %q, want %d characters", meeting.RoomCode, roomCodeLength)
	}
	for _, char := range meeting.RoomCode {
		if !strings.ContainsRune(testRoomCodeAlphabet, char) {
			t.Fatalf("roomCode = %q contains %q", meeting.RoomCode, char)
		}
	}
	if meeting.MeetingId == "" {
		t.Fatal("meetingId is empty")
	}
	if want := now.Add(time.Hour); !meeting.ExpiresAt.Equal(want) {
		t.Fatalf("expiresAt = %s, want %s", meeting.ExpiresAt, want)
	}

	claims, err := service.signer.verify(meeting.JoinToken, now)
	if err != nil {
		t.Fatalf("verify host token: %v", err)
	}
	if claims.RoomCode != meeting.RoomCode || claims.MeetingID != meeting.MeetingId {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Role != Host || claims.ParticipantID != "" {
		t.Fatalf("host claims = %+v, want role host and no participant", claims)
	}
}

func TestCreateMeetingRejectsEmptyHostName(t *testing.T) {
	service := newTestRoomSession()

	if _, err := service.Create(MeetingCreateRequest{HostDisplayName: "  "}); !errors.Is(err, ErrInvalidRoomRequest) {
		t.Fatalf("err = %v, want ErrInvalidRoomRequest", err)
	}
}

func TestJoinIssuesParticipantTokenAndPlaceholder(t *testing.T) {
	service := newTestRoomSession()
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }
	meeting := createMeeting(t, service, "Alice")

	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	if host.ParticipantId == "" {
		t.Fatal("participantId is empty")
	}
	if host.Subscriptions == nil || len(host.Subscriptions) != 0 {
		t.Fatalf("subscriptions = %+v, want a non-nil empty placeholder", host.Subscriptions)
	}
	if !strings.Contains(host.PublishUrl, host.ParticipantId) {
		t.Fatalf("publishUrl = %q, want the participant id", host.PublishUrl)
	}

	claims, err := service.signer.verify(host.JoinToken, now)
	if err != nil {
		t.Fatalf("verify participant token: %v", err)
	}
	if claims.ParticipantID != host.ParticipantId || claims.Role != Host {
		t.Fatalf("claims = %+v, want the host participant", claims)
	}

	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")
	guestClaims, err := service.signer.verify(guest.JoinToken, now)
	if err != nil {
		t.Fatalf("verify guest token: %v", err)
	}
	if guestClaims.Role != Guest || guestClaims.ParticipantID != guest.ParticipantId {
		t.Fatalf("guest claims = %+v", guestClaims)
	}
}

func TestJoinRejectsExpiredHostToken(t *testing.T) {
	service := newTestRoomSession()
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }
	meeting := createMeeting(t, service, "Alice")

	service.now = func() time.Time { return now.Add(2 * time.Hour) }
	_, err := service.Join(meeting.RoomCode, RoomJoinRequest{DisplayName: "Alice"}, meeting.JoinToken)
	if !errors.Is(err, ErrExpiredJoinToken) {
		t.Fatalf("err = %v, want ErrExpiredJoinToken", err)
	}
}

func TestJoinRejectsTokenForAnotherRoom(t *testing.T) {
	service := newTestRoomSession()
	first := createMeeting(t, service, "Alice")
	second := createMeeting(t, service, "Bob")

	_, err := service.Join(second.RoomCode, RoomJoinRequest{DisplayName: "Alice"}, first.JoinToken)
	if !errors.Is(err, ErrInvalidJoinToken) {
		t.Fatalf("err = %v, want ErrInvalidJoinToken", err)
	}
}

func TestJoinWithParticipantIDRequiresMatchingToken(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	rejoin := RoomJoinRequest{DisplayName: "Bob", ParticipantId: &guest.ParticipantId}
	if _, err := service.Join(meeting.RoomCode, rejoin, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("join without token: err = %v, want ErrForbidden", err)
	}

	rejoined, err := service.Join(meeting.RoomCode, rejoin, guest.JoinToken)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if rejoined.ParticipantId != guest.ParticipantId {
		t.Fatalf("participantId = %q, want %q", rejoined.ParticipantId, guest.ParticipantId)
	}
}

func TestStatusReturnsRoster(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	joinRoom(t, service, meeting.RoomCode, "Bob", "")

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Open {
		t.Fatalf("state = %q, want open", status.State)
	}
	if len(status.Participants) != 2 {
		t.Fatalf("participants = %+v, want 2", status.Participants)
	}

	roles := map[string]RosterEntryRole{}
	for _, entry := range status.Participants {
		roles[entry.DisplayName] = entry.Role
		if entry.Tracks == nil {
			t.Fatalf("%s tracks is nil, want an empty array", entry.DisplayName)
		}
	}
	if roles["Alice"] != Host || roles["Bob"] != Guest {
		t.Fatalf("roles = %v", roles)
	}
}

func TestStatusRejectsHostTokenWithoutParticipant(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")

	if _, err := service.Status(meeting.RoomCode, meeting.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestLeaveRequiresOwnToken(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.Leave(meeting.RoomCode, guest.ParticipantId, host.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("leave with host token: err = %v, want ErrForbidden", err)
	}
	if err := service.Leave(meeting.RoomCode, guest.ParticipantId, guest.JoinToken); err != nil {
		t.Fatalf("leave: %v", err)
	}

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Participants) != 1 || status.Participants[0].DisplayName != "Alice" {
		t.Fatalf("participants = %+v, want only Alice", status.Participants)
	}
}

func TestRoomExpires(t *testing.T) {
	service := newTestRoomSession()
	now := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return now }
	meeting := createMeeting(t, service, "Alice")

	service.now = func() time.Time { return now.Add(2 * time.Hour) }
	token, err := service.signer.issue(joinClaims{
		RoomCode:      meeting.RoomCode,
		MeetingID:     meeting.MeetingId,
		ParticipantID: "p1",
		Role:          Guest,
		ExpiresAt:     now.Add(3 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	status, err := service.Status(meeting.RoomCode, token)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.State != Closed {
		t.Fatalf("state = %q, want closed", status.State)
	}
	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{DisplayName: "Bob"}, ""); !errors.Is(err, ErrRoomClosed) {
		t.Fatalf("join: err = %v, want ErrRoomClosed", err)
	}
}

func TestUnknownRoomIsNotFound(t *testing.T) {
	service := newTestRoomSession()

	if _, err := service.Join("ZZZZZZ", RoomJoinRequest{DisplayName: "Bob"}, ""); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("join err = %v, want ErrRoomNotFound", err)
	}
	if _, err := service.Status("ZZZZZZ", "token"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("status err = %v, want ErrRoomNotFound", err)
	}
	if err := service.Leave("ZZZZZZ", "p1", "token"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("leave err = %v, want ErrRoomNotFound", err)
	}
}
