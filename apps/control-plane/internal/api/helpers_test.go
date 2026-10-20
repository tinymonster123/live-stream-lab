package api

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/broadcast"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/room"
)

// stubRunner satisfies broadcast.ProcessRunner. The process stays alive until
// it is signalled, so a started Broadcast reports live.
type stubRunner struct{}

func (stubRunner) Start([]string) (broadcast.Process, error) {
	return newStubProcess(), nil
}

type stubProcess struct {
	done chan struct{}
	once sync.Once
}

func newStubProcess() *stubProcess {
	return &stubProcess{done: make(chan struct{})}
}

func (process *stubProcess) Done() <-chan struct{} { return process.done }

func (process *stubProcess) Err() error { return nil }

func (process *stubProcess) Signal(os.Signal) error {
	process.once.Do(func() { close(process.done) })
	return nil
}

func (process *stubProcess) Kill() error {
	process.once.Do(func() { close(process.done) })
	return nil
}

type stubProbe struct {
	viewers int
}

func (probe *stubProbe) WaitForPublish(context.Context, string, time.Duration) error {
	return nil
}

func (probe *stubProbe) EnsureUnpublished(context.Context, string, time.Duration) error {
	return nil
}

func (probe *stubProbe) ViewerCount(context.Context, string) (int, error) {
	return probe.viewers, nil
}

func testBroadcastConfig() broadcast.Config {
	return broadcast.Config{
		RTMPBase: "rtmp://127.0.0.1:1935/live",
		FLVBase:  "http://127.0.0.1:8080/live",
		HLSBase:  "http://127.0.0.1:8080/live",
	}
}

func newTestBroadcasts() *broadcast.Service {
	return broadcast.New(stubRunner{}, nil, testBroadcastConfig())
}

func newTestRooms() *room.Session {
	return newTestRoomsWith(room.Config{
		JoinSecret: []byte("test-secret"),
		WhipBase:   "/rtc/v1/whip/",
		WhepBase:   "/rtc/v1/whep/",
		TTL:        time.Hour,
	})
}

// newTestRoomsWithHeartbeatTimeout keeps the real lifecycle ticker but makes
// the offline transition fast enough to observe over a WebSocket.
func newTestRoomsWithHeartbeatTimeout(timeout time.Duration) *room.Session {
	return newTestRoomsWith(room.Config{
		JoinSecret:       []byte("test-secret"),
		WhipBase:         "/rtc/v1/whip/",
		WhepBase:         "/rtc/v1/whep/",
		TTL:              time.Hour,
		HeartbeatTimeout: timeout,
	})
}

func newTestRoomsWith(config room.Config) *room.Session {
	rooms, err := room.New(config)
	if err != nil {
		panic(err)
	}
	return rooms
}

func newTestMux() *http.ServeMux {
	return NewMux(newTestBroadcasts(), newTestRooms())
}

func createMeeting(t *testing.T, rooms *room.Session, host string) contracts.MeetingCreated {
	t.Helper()

	meeting, err := rooms.Create(contracts.MeetingCreateRequest{HostDisplayName: host})
	if err != nil {
		t.Fatalf("Create meeting: %v", err)
	}
	return meeting
}

func joinRoom(t *testing.T, rooms *room.Session, code, name, bearer string) contracts.JoinGrant {
	t.Helper()

	grant, err := rooms.Join(code, contracts.RoomJoinRequest{DisplayName: name}, bearer)
	if err != nil {
		t.Fatalf("Join %q: %v", name, err)
	}
	return grant
}

func publishTracks(
	t *testing.T,
	rooms *room.Session,
	code, participantID, token string,
	tracks ...contracts.RoomTrack,
) {
	t.Helper()

	err := rooms.UpdateTracks(code, contracts.RoomTrackUpdate{
		ParticipantId: participantID,
		Tracks:        tracks,
	}, token)
	if err != nil {
		t.Fatalf("UpdateTracks: %v", err)
	}
}
