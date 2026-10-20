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
		WhipBase:   "/rtc/v1/whip/",
		WhepBase:   "/rtc/v1/whep/",
		TTL:        time.Hour,
	})
	if err != nil {
		panic(err)
	}
	return service
}

func publishTracks(t *testing.T, service *RoomSession, code, participantID, token string, tracks ...RoomTrack) {
	t.Helper()

	err := service.UpdateTracks(code, RoomTrackUpdate{
		ParticipantId: participantID,
		Tracks:        tracks,
	}, token)
	if err != nil {
		t.Fatalf("UpdateTracks: %v", err)
	}
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

func TestJoinIssuesParticipantTokenAndWhipURL(t *testing.T) {
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

func TestJoinReturnsSubscriptionsForPublishedTracks(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	if want := "/rtc/v1/whip/?app=live&stream=" + host.ParticipantId; host.PublishUrl != want {
		t.Fatalf("publishUrl = %q, want %q", host.PublishUrl, want)
	}
	if len(host.Subscriptions) != 0 {
		t.Fatalf("subscriptions = %+v, want none for the first participant", host.Subscriptions)
	}

	publishTracks(t, service, meeting.RoomCode, host.ParticipantId, host.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
		RoomTrack{TrackId: "microphone", Kind: RoomTrackKindAudio},
	)

	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")
	if len(guest.Subscriptions) != 2 {
		t.Fatalf("subscriptions = %+v, want one per published Track", guest.Subscriptions)
	}
	whep := "/rtc/v1/whep/?app=live&stream=" + host.ParticipantId
	for index, trackID := range []string{"camera", "microphone"} {
		target := guest.Subscriptions[index]
		if target.ParticipantId != host.ParticipantId || target.TrackId != trackID || target.WhepUrl != whep {
			t.Fatalf("subscriptions[%d] = %+v", index, target)
		}
	}

	status, err := service.Status(meeting.RoomCode, guest.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	hostEntry := status.Participants[0]
	if hostEntry.ParticipantId != host.ParticipantId || len(hostEntry.Tracks) != 2 {
		t.Fatalf("host roster entry = %+v, want two tracks", hostEntry)
	}
	if hostEntry.Tracks[0].TrackId != "camera" || hostEntry.Tracks[0].Kind != TrackSummaryKindVideo {
		t.Fatalf("track = %+v", hostEntry.Tracks[0])
	}
	if hostEntry.Tracks[0].Published == nil || !*hostEntry.Tracks[0].Published {
		t.Fatalf("track published = %v, want true", hostEntry.Tracks[0].Published)
	}
}

func TestUpdateTracksReplacesTheReportedSet(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	publishTracks(t, service, meeting.RoomCode, host.ParticipantId, host.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
		RoomTrack{TrackId: "microphone", Kind: RoomTrackKindAudio},
	)
	publishTracks(t, service, meeting.RoomCode, host.ParticipantId, host.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
	)

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	tracks := status.Participants[0].Tracks
	if len(tracks) != 1 || tracks[0].TrackId != "camera" {
		t.Fatalf("tracks = %+v, want only the camera", tracks)
	}
}

func TestUpdateTracksRequiresOwnToken(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	tracks := []RoomTrack{{TrackId: "camera", Kind: RoomTrackKindVideo}}
	cases := []struct {
		name  string
		token string
	}{
		{name: "missing", token: ""},
		{name: "another participant", token: host.JoinToken},
		{name: "host token", token: meeting.JoinToken},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := service.UpdateTracks(meeting.RoomCode, RoomTrackUpdate{
				ParticipantId: guest.ParticipantId,
				Tracks:        tracks,
			}, test.token)
			if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrInvalidJoinToken) {
				t.Fatalf("err = %v, want a forbidden token error", err)
			}
		})
	}

	if err := service.UpdateTracks("ZZZZZZ", RoomTrackUpdate{
		ParticipantId: guest.ParticipantId,
		Tracks:        tracks,
	}, guest.JoinToken); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("unknown room err = %v, want ErrRoomNotFound", err)
	}
}

func TestUpdateTracksValidatesInput(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)

	cases := []struct {
		name   string
		tracks []RoomTrack
	}{
		{name: "empty track id", tracks: []RoomTrack{{Kind: RoomTrackKindVideo}}},
		{name: "unknown kind", tracks: []RoomTrack{{TrackId: "camera", Kind: "smell"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := service.UpdateTracks(meeting.RoomCode, RoomTrackUpdate{
				ParticipantId: host.ParticipantId,
				Tracks:        test.tracks,
			}, host.JoinToken)
			if !errors.Is(err, ErrInvalidRoomRequest) {
				t.Fatalf("err = %v, want ErrInvalidRoomRequest", err)
			}
		})
	}
}

func TestRejoinUnpublishesStaleTracks(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	publishTracks(t, service, meeting.RoomCode, guest.ParticipantId, guest.JoinToken,
		RoomTrack{TrackId: "camera", Kind: RoomTrackKindVideo},
	)

	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Bob",
		ParticipantId: &guest.ParticipantId,
	}, guest.JoinToken); err != nil {
		t.Fatalf("rejoin: %v", err)
	}

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, entry := range status.Participants {
		if entry.ParticipantId == guest.ParticipantId && len(entry.Tracks) != 0 {
			t.Fatalf("guest tracks after rejoin = %+v, want none", entry.Tracks)
		}
	}
}

func TestRejoinAfterLeaveKeepsParticipantID(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.Leave(meeting.RoomCode, guest.ParticipantId, guest.JoinToken); err != nil {
		t.Fatalf("leave: %v", err)
	}

	rejoined, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Bob",
		ParticipantId: &guest.ParticipantId,
	}, guest.JoinToken)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if rejoined.ParticipantId != guest.ParticipantId {
		t.Fatalf("participantId = %q, want the original %q", rejoined.ParticipantId, guest.ParticipantId)
	}

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Participants) != 2 {
		t.Fatalf("participants = %+v, want Alice and the rejoined Bob", status.Participants)
	}
	var bob *RosterEntry
	for index := range status.Participants {
		if status.Participants[index].ParticipantId == guest.ParticipantId {
			bob = &status.Participants[index]
		}
	}
	if bob == nil {
		t.Fatal("rejoined participant is missing from the Roster")
	}
	if bob.DisplayName != "Bob" || len(bob.Tracks) != 0 {
		t.Fatalf("rejoined entry = %+v, want Bob with no Tracks", bob)
	}
}

func TestMuteUpdatesTheRoster(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	mute := func(muted bool, token string) error {
		return service.UpdateMute(meeting.RoomCode, RoomMuteUpdate{
			ParticipantId: guest.ParticipantId,
			Muted:         muted,
		}, token)
	}
	mutedState := func() bool {
		t.Helper()
		status, err := service.Status(meeting.RoomCode, host.JoinToken)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		for _, entry := range status.Participants {
			if entry.ParticipantId == guest.ParticipantId {
				return entry.Muted
			}
		}
		t.Fatal("guest is missing from the Roster")
		return false
	}

	if err := mute(true, guest.JoinToken); err != nil {
		t.Fatalf("mute: %v", err)
	}
	if !mutedState() {
		t.Fatal("guest is not muted after UpdateMute")
	}
	if err := mute(false, guest.JoinToken); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	if mutedState() {
		t.Fatal("guest is still muted after unmuting")
	}

	if err := mute(true, host.JoinToken); !errors.Is(err, ErrForbidden) {
		t.Fatalf("muting another participant: err = %v, want ErrForbidden", err)
	}
	if err := service.UpdateMute("ZZZZZZ", RoomMuteUpdate{ParticipantId: guest.ParticipantId}, guest.JoinToken); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("unknown room: err = %v, want ErrRoomNotFound", err)
	}
}

func TestRejoinResetsMute(t *testing.T) {
	service := newTestRoomSession()
	meeting := createMeeting(t, service, "Alice")
	host := joinRoom(t, service, meeting.RoomCode, "Alice", meeting.JoinToken)
	guest := joinRoom(t, service, meeting.RoomCode, "Bob", "")

	if err := service.UpdateMute(meeting.RoomCode, RoomMuteUpdate{
		ParticipantId: guest.ParticipantId,
		Muted:         true,
	}, guest.JoinToken); err != nil {
		t.Fatalf("mute: %v", err)
	}

	if _, err := service.Join(meeting.RoomCode, RoomJoinRequest{
		DisplayName:   "Bob",
		ParticipantId: &guest.ParticipantId,
	}, guest.JoinToken); err != nil {
		t.Fatalf("rejoin: %v", err)
	}

	status, err := service.Status(meeting.RoomCode, host.JoinToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, entry := range status.Participants {
		if entry.ParticipantId == guest.ParticipantId && entry.Muted {
			t.Fatalf("rejoined entry is still muted: %+v", entry)
		}
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
