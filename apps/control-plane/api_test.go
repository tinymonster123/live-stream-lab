package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestMux() *http.ServeMux {
	return newMux(newTestService(&fakeRunner{}), newTestRoomSession())
}

func performRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return performAuthorizedRequest(t, mux, method, path, body, "")
}

func performAuthorizedRequest(
	t *testing.T,
	mux *http.ServeMux,
	method, path, body, token string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func decodeSession(t *testing.T, recorder *httptest.ResponseRecorder) BroadcastSession {
	t.Helper()
	return decodeJSONBody[BroadcastSession](t, recorder)
}

func TestCreateBroadcastEndpoint(t *testing.T) {
	mux := newTestMux()
	body := `{"source":{"type":"file","path":"/tmp/fixture.mp4","loop":true},"latencyClass":"low"}`

	recorder := performRequest(t, mux, http.MethodPost, "/api/v1/broadcasts", body)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	session := decodeSession(t, recorder)
	if session.BroadcastId == "" {
		t.Fatal("broadcastId is empty")
	}
	if session.Publish.RtmpUrl == "" || session.Publish.StreamKey == "" {
		t.Fatalf("publish descriptor = %+v", session.Publish)
	}
	if session.Playback.Url == "" {
		t.Fatalf("playback descriptor = %+v", session.Playback)
	}
}

func TestBroadcastLifecycleEndpoints(t *testing.T) {
	mux := newTestMux()
	created := decodeSession(t, performRequest(
		t, mux, http.MethodPost, "/api/v1/broadcasts",
		`{"source":{"type":"file","path":"/tmp/fixture.mp4"},"latencyClass":"low"}`,
	))
	path := "/api/v1/broadcasts/" + created.BroadcastId

	started := performRequest(t, mux, http.MethodPost, path+"/start", "")
	if started.Code != http.StatusOK {
		t.Fatalf("start status = %d (body %s)", started.Code, started.Body.String())
	}
	if session := decodeSession(t, started); session.Status != BroadcastSessionStatusLive {
		t.Fatalf("status after start = %q, want live", session.Status)
	}

	startedAgain := performRequest(t, mux, http.MethodPost, path+"/start", "")
	if startedAgain.Code != http.StatusOK {
		t.Fatalf("second start status = %d", startedAgain.Code)
	}

	status := performRequest(t, mux, http.MethodGet, path, "")
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d", status.Code)
	}
	if session := decodeSession(t, status); session.Status != BroadcastSessionStatusLive {
		t.Fatalf("status = %q, want live", session.Status)
	}

	stopped := performRequest(t, mux, http.MethodPost, path+"/stop", "")
	if stopped.Code != http.StatusOK {
		t.Fatalf("stop status = %d", stopped.Code)
	}
	if session := decodeSession(t, stopped); session.Status != BroadcastSessionStatusStopped {
		t.Fatalf("status after stop = %q, want stopped", session.Status)
	}

	stoppedAgain := performRequest(t, mux, http.MethodPost, path+"/stop", "")
	if stoppedAgain.Code != http.StatusOK {
		t.Fatalf("second stop status = %d", stoppedAgain.Code)
	}
}

func TestCreateBroadcastWithStandardLatencyReturnsHLS(t *testing.T) {
	mux := newTestMux()
	body := `{"source":{"type":"file","path":"/tmp/fixture.mp4"},"latencyClass":"standard"}`

	recorder := performRequest(t, mux, http.MethodPost, "/api/v1/broadcasts", body)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	session := decodeSession(t, recorder)
	if session.Playback.Protocol != Hls {
		t.Fatalf("protocol = %q, want hls", session.Playback.Protocol)
	}
	if len(session.Playback.Fallbacks) != 0 {
		t.Fatalf("fallbacks = %+v, want none", session.Playback.Fallbacks)
	}
}

func TestGetBroadcastReportsViewerCount(t *testing.T) {
	probe := &fakeProbe{viewers: 5}
	mux := newMux(NewBroadcastService(&fakeRunner{}, probe, testConfig()), newTestRoomSession())
	created := decodeSession(t, performRequest(
		t, mux, http.MethodPost, "/api/v1/broadcasts",
		`{"source":{"type":"file","path":"/tmp/fixture.mp4"},"latencyClass":"low"}`,
	))
	path := "/api/v1/broadcasts/" + created.BroadcastId

	if recorder := performRequest(t, mux, http.MethodPost, path+"/start", ""); recorder.Code != http.StatusOK {
		t.Fatalf("start status = %d (body %s)", recorder.Code, recorder.Body.String())
	}

	status := performRequest(t, mux, http.MethodGet, path, "")
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d", status.Code)
	}
	session := decodeSession(t, status)
	if session.ViewerCount != 5 {
		t.Fatalf("viewerCount = %d, want 5", session.ViewerCount)
	}
	if session.Status != BroadcastSessionStatusLive {
		t.Fatalf("status = %q, want live", session.Status)
	}
}

func TestUnknownBroadcastReturnsNotFound(t *testing.T) {
	mux := newTestMux()

	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/broadcasts/missing"},
		{http.MethodPost, "/api/v1/broadcasts/missing/start"},
		{http.MethodPost, "/api/v1/broadcasts/missing/stop"},
	} {
		recorder := performRequest(t, mux, test.method, test.path, "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", test.method, test.path, recorder.Code)
		}
		assertErrorCode(t, recorder, "not_found")
	}
}

func TestInvalidCreateRequestReturnsBadRequest(t *testing.T) {
	mux := newTestMux()

	for _, body := range []string{
		`{`,
		`{"source":{"type":"file","path":"/tmp/a.mp4"},"latencyClass":"ultra-low"}`,
		`{"source":{"type":"file"},"latencyClass":"low"}`,
	} {
		recorder := performRequest(t, mux, http.MethodPost, "/api/v1/broadcasts", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, recorder.Code)
		}
	}
}

func decodeJSONBody[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()

	var body T
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, recorder.Body.String())
	}
	return body
}

func TestMeetingLifecycleEndpoints(t *testing.T) {
	mux := newTestMux()

	created := performRequest(t, mux, http.MethodPost, "/api/v1/meetings", `{"hostDisplayName":"Alice"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", created.Code, created.Body.String())
	}
	meeting := decodeJSONBody[MeetingCreated](t, created)
	if meeting.RoomCode == "" || meeting.JoinToken == "" || meeting.MeetingId == "" {
		t.Fatalf("meeting = %+v", meeting)
	}
	path := "/api/v1/rooms/" + meeting.RoomCode

	hostResponse := performAuthorizedRequest(
		t, mux, http.MethodPost, path+"/join", `{"displayName":"Alice"}`, meeting.JoinToken,
	)
	if hostResponse.Code != http.StatusOK {
		t.Fatalf("host join status = %d (body %s)", hostResponse.Code, hostResponse.Body.String())
	}
	host := decodeJSONBody[JoinGrant](t, hostResponse)
	if host.ParticipantId == "" || host.JoinToken == "" || host.PublishUrl == "" {
		t.Fatalf("host grant = %+v", host)
	}
	if host.Subscriptions == nil {
		t.Fatal("host subscriptions is nil, want an empty array")
	}

	guestResponse := performRequest(
		t, mux, http.MethodPost, path+"/join", `{"displayName":"Bob"}`,
	)
	if guestResponse.Code != http.StatusOK {
		t.Fatalf("guest join status = %d (body %s)", guestResponse.Code, guestResponse.Body.String())
	}
	guest := decodeJSONBody[JoinGrant](t, guestResponse)

	statusResponse := performAuthorizedRequest(t, mux, http.MethodGet, path, "", host.JoinToken)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status code = %d (body %s)", statusResponse.Code, statusResponse.Body.String())
	}
	status := decodeJSONBody[RoomStatus](t, statusResponse)
	if status.State != Open || len(status.Participants) != 2 {
		t.Fatalf("status = %+v, want open with 2 participants", status)
	}

	leaveResponse := performAuthorizedRequest(
		t,
		mux,
		http.MethodPost,
		path+"/leave",
		`{"participantId":"`+guest.ParticipantId+`"}`,
		guest.JoinToken,
	)
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("leave status = %d, want 204 (body %s)", leaveResponse.Code, leaveResponse.Body.String())
	}

	statusResponse = performAuthorizedRequest(t, mux, http.MethodGet, path, "", host.JoinToken)
	status = decodeJSONBody[RoomStatus](t, statusResponse)
	if len(status.Participants) != 1 || status.Participants[0].DisplayName != "Alice" {
		t.Fatalf("participants after leave = %+v", status.Participants)
	}
}

func TestUpdateRoomTracksEndpoint(t *testing.T) {
	mux := newTestMux()
	meeting := decodeJSONBody[MeetingCreated](t, performRequest(
		t, mux, http.MethodPost, "/api/v1/meetings", `{"hostDisplayName":"Alice"}`,
	))
	path := "/api/v1/rooms/" + meeting.RoomCode
	host := decodeJSONBody[JoinGrant](t, performAuthorizedRequest(
		t, mux, http.MethodPost, path+"/join", `{"displayName":"Alice"}`, meeting.JoinToken,
	))

	body := `{"participantId":"` + host.ParticipantId +
		`","tracks":[{"trackId":"camera","kind":"video"},{"trackId":"microphone","kind":"audio"}]}`
	updated := performAuthorizedRequest(t, mux, http.MethodPut, path+"/tracks", body, host.JoinToken)
	if updated.Code != http.StatusNoContent {
		t.Fatalf("update tracks = %d, want 204 (body %s)", updated.Code, updated.Body.String())
	}

	statusResponse := performAuthorizedRequest(t, mux, http.MethodGet, path, "", host.JoinToken)
	status := decodeJSONBody[RoomStatus](t, statusResponse)
	if len(status.Participants) != 1 || len(status.Participants[0].Tracks) != 2 {
		t.Fatalf("status = %+v, want two tracks", status.Participants)
	}

	missingToken := performRequest(t, mux, http.MethodPut, path+"/tracks", body)
	if missingToken.Code != http.StatusForbidden {
		t.Fatalf("update without token = %d, want 403", missingToken.Code)
	}

	invalid := performAuthorizedRequest(t, mux, http.MethodPut, path+"/tracks",
		`{"participantId":"`+host.ParticipantId+`","tracks":[{"trackId":"","kind":"video"}]}`,
		host.JoinToken)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid tracks = %d, want 400 (body %s)", invalid.Code, invalid.Body.String())
	}

	unknown := performRequest(t, mux, http.MethodPut, "/api/v1/rooms/ZZZZZZ/tracks", body)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown room = %d, want 404", unknown.Code)
	}
}

func TestRoomEndpointsRequireAValidJoinToken(t *testing.T) {
	mux := newTestMux()
	meeting := decodeJSONBody[MeetingCreated](t, performRequest(
		t, mux, http.MethodPost, "/api/v1/meetings", `{"hostDisplayName":"Alice"}`,
	))
	path := "/api/v1/rooms/" + meeting.RoomCode
	host := decodeJSONBody[JoinGrant](t, performAuthorizedRequest(
		t, mux, http.MethodPost, path+"/join", `{"displayName":"Alice"}`, meeting.JoinToken,
	))

	status := performRequest(t, mux, http.MethodGet, path, "")
	if status.Code != http.StatusForbidden {
		t.Fatalf("status without token = %d, want 403", status.Code)
	}
	assertErrorCode(t, status, "forbidden")

	hostTokenStatus := performAuthorizedRequest(t, mux, http.MethodGet, path, "", meeting.JoinToken)
	if hostTokenStatus.Code != http.StatusForbidden {
		t.Fatalf("status with the meeting host token = %d, want 403", hostTokenStatus.Code)
	}

	leave := performAuthorizedRequest(t, mux, http.MethodPost, path+"/leave",
		`{"participantId":"`+host.ParticipantId+`"}`, "not-a-token")
	if leave.Code != http.StatusForbidden {
		t.Fatalf("leave with bad token = %d, want 403", leave.Code)
	}

	join := performAuthorizedRequest(t, mux, http.MethodPost, path+"/join",
		`{"displayName":"Bob"}`, "not-a-token")
	if join.Code != http.StatusForbidden {
		t.Fatalf("join with bad token = %d, want 403", join.Code)
	}
}

func TestUnknownRoomEndpoints(t *testing.T) {
	mux := newTestMux()

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/rooms/ZZZZZZ/join", `{"displayName":"Bob"}`},
		{http.MethodGet, "/api/v1/rooms/ZZZZZZ", ""},
		{http.MethodPost, "/api/v1/rooms/ZZZZZZ/leave", `{"participantId":"p1"}`},
	} {
		recorder := performRequest(t, mux, request.method, request.path, request.body)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404 (body %s)",
				request.method, request.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestMeetingCreateValidation(t *testing.T) {
	mux := newTestMux()

	for _, body := range []string{
		`{`,
		`{}`,
		`{"hostDisplayName":"  "}`,
	} {
		recorder := performRequest(t, mux, http.MethodPost, "/api/v1/meetings", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, recorder.Code)
		}
	}
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, code string) {
	t.Helper()

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != code {
		t.Fatalf("error code = %q, want %q", body.Code, code)
	}
	if body.Message == "" {
		t.Fatal("error message is empty")
	}
}
