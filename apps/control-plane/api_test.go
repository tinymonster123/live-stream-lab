package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func performRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func decodeSession(t *testing.T, recorder *httptest.ResponseRecorder) BroadcastSession {
	t.Helper()

	var session BroadcastSession
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v (body %s)", err, recorder.Body.String())
	}
	return session
}

func TestCreateBroadcastEndpoint(t *testing.T) {
	mux := newMux(newTestService(&fakeRunner{}))
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
	mux := newMux(newTestService(&fakeRunner{}))
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

func TestUnknownBroadcastReturnsNotFound(t *testing.T) {
	mux := newMux(newTestService(&fakeRunner{}))

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
	mux := newMux(newTestService(&fakeRunner{}))

	for _, body := range []string{
		`{`,
		`{"source":{"type":"file","path":"/tmp/a.mp4"},"latencyClass":"standard"}`,
		`{"source":{"type":"file"},"latencyClass":"low"}`,
	} {
		recorder := performRequest(t, mux, http.MethodPost, "/api/v1/broadcasts", body)
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
