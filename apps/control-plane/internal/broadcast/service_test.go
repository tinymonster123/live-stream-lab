package broadcast

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

const (
	testRTMPBase = "rtmp://127.0.0.1:1935/live"
	testFLVBase  = "http://127.0.0.1:8080/live"
	testHLSBase  = "http://127.0.0.1:8080/hls"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	procs []*fakeProcess
	err   error
}

func (r *fakeRunner) Start(args []string) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.err != nil {
		return nil, r.err
	}

	process := newFakeProcess()
	r.calls = append(r.calls, append([]string(nil), args...))
	r.procs = append(r.procs, process)
	return process, nil
}

func (r *fakeRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *fakeRunner) call(index int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[index]
}

func (r *fakeRunner) process(index int) *fakeProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[index]
}

type fakeProcess struct {
	mu      sync.Mutex
	once    sync.Once
	done    chan struct{}
	err     error
	signals []os.Signal
	killed  bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan struct{})}
}

func (p *fakeProcess) Done() <-chan struct{} { return p.done }

func (p *fakeProcess) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *fakeProcess) Signal(signal os.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, signal)
	p.mu.Unlock()
	go p.finish(nil)
	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.finish(errors.New("killed"))
	return nil
}

func (p *fakeProcess) finish(err error) {
	p.once.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	})
}

func (p *fakeProcess) signalCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.signals)
}

type fakeProbe struct {
	err            error
	unpublishErr   error
	viewerErr      error
	viewers        int
	calls          []string
	unpublishCalls []string
	viewerCalls    []string
}

func (p *fakeProbe) WaitForPublish(_ context.Context, streamKey string, _ time.Duration) error {
	p.calls = append(p.calls, streamKey)
	return p.err
}

func (p *fakeProbe) EnsureUnpublished(_ context.Context, streamKey string, _ time.Duration) error {
	p.unpublishCalls = append(p.unpublishCalls, streamKey)
	return p.unpublishErr
}

func (p *fakeProbe) ViewerCount(_ context.Context, streamKey string) (int, error) {
	p.viewerCalls = append(p.viewerCalls, streamKey)
	return p.viewers, p.viewerErr
}

func testConfig() Config {
	return Config{RTMPBase: testRTMPBase, FLVBase: testFLVBase, HLSBase: testHLSBase}
}

func newTestService(runner ProcessRunner) *Service {
	return New(runner, &fakeProbe{}, testConfig())
}

func stringPointer(value string) *string { return &value }

func boolPointer(value bool) *bool { return &value }

func fileSpec() contracts.BroadcastCreateRequest {
	return contracts.BroadcastCreateRequest{
		Source:       contracts.BroadcastSource{Type: contracts.File, Path: stringPointer("/tmp/fixture.mp4")},
		LatencyClass: contracts.Low,
	}
}

func TestCreateReturnsPublishAndPlayback(t *testing.T) {
	service := newTestService(&fakeRunner{})

	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if session.Status != contracts.BroadcastSessionStatusIdle {
		t.Fatalf("status = %q, want idle", session.Status)
	}
	if session.Publish.RtmpUrl != testRTMPBase {
		t.Fatalf("rtmpUrl = %q, want %q", session.Publish.RtmpUrl, testRTMPBase)
	}
	if session.Publish.StreamKey != session.BroadcastId {
		t.Fatalf("streamKey = %q, want broadcast id %q", session.Publish.StreamKey, session.BroadcastId)
	}
	if session.Playback.Protocol != contracts.Flv {
		t.Fatalf("playback protocol = %q, want flv", session.Playback.Protocol)
	}
	wantURL := testFLVBase + "/" + session.BroadcastId + ".flv"
	if session.Playback.Url != wantURL {
		t.Fatalf("playback url = %q, want %q", session.Playback.Url, wantURL)
	}
	if session.Playback.ExpiresAt <= int(time.Now().UnixMilli()) {
		t.Fatalf("expiresAt = %d, want a future timestamp", session.Playback.ExpiresAt)
	}
	if len(session.Playback.Fallbacks) != 1 {
		t.Fatalf("fallbacks = %+v, want one HLS fallback", session.Playback.Fallbacks)
	}
	fallback := session.Playback.Fallbacks[0]
	if fallback.Protocol != contracts.Hls {
		t.Fatalf("fallback protocol = %q, want hls", fallback.Protocol)
	}
	wantHLS := testHLSBase + "/" + session.BroadcastId + ".m3u8"
	if fallback.Url != wantHLS {
		t.Fatalf("fallback url = %q, want %q", fallback.Url, wantHLS)
	}
}

func TestCreateWithStandardLatencyUsesHLS(t *testing.T) {
	service := newTestService(&fakeRunner{})
	spec := fileSpec()
	spec.LatencyClass = contracts.Standard

	session, err := service.Create(spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if session.Playback.Protocol != contracts.Hls {
		t.Fatalf("playback protocol = %q, want hls", session.Playback.Protocol)
	}
	wantURL := testHLSBase + "/" + session.BroadcastId + ".m3u8"
	if session.Playback.Url != wantURL {
		t.Fatalf("playback url = %q, want %q", session.Playback.Url, wantURL)
	}
	if len(session.Playback.Fallbacks) != 0 {
		t.Fatalf("fallbacks = %+v, want none for standard HLS", session.Playback.Fallbacks)
	}
}

func TestCreateRejectsInvalidSpecs(t *testing.T) {
	tests := []struct {
		name string
		spec contracts.BroadcastCreateRequest
		want error
	}{
		{
			name: "unknown source type",
			spec: contracts.BroadcastCreateRequest{
				Source:       contracts.BroadcastSource{Type: contracts.BroadcastSourceType("ndi")},
				LatencyClass: contracts.Low,
			},
			want: ErrInvalid,
		},
		{
			name: "file without path",
			spec: contracts.BroadcastCreateRequest{
				Source:       contracts.BroadcastSource{Type: contracts.File},
				LatencyClass: contracts.Low,
			},
			want: ErrInvalid,
		},
		{
			name: "ultra-low latency has no protocol yet",
			spec: contracts.BroadcastCreateRequest{
				Source:       contracts.BroadcastSource{Type: contracts.File, Path: stringPointer("/tmp/a.mp4")},
				LatencyClass: contracts.UltraLow,
			},
			want: ErrUnsupportedLatency,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(&fakeRunner{})
			if _, err := service.Create(test.spec); !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStartIsIdempotent(t *testing.T) {
	runner := &fakeRunner{}
	service := newTestService(runner)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := service.Start(session.BroadcastId)
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	second, err := service.Start(session.BroadcastId)
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}

	if first.Status != contracts.BroadcastSessionStatusLive || second.Status != contracts.BroadcastSessionStatusLive {
		t.Fatalf("statuses = %q, %q, want live/live", first.Status, second.Status)
	}
	if runner.callCount() != 1 {
		t.Fatalf("ffmpeg starts = %d, want 1", runner.callCount())
	}
	if first.StartedAt == nil {
		t.Fatal("startedAt is nil after Start")
	}
}

func TestStartWaitsForIngest(t *testing.T) {
	runner := &fakeRunner{}
	probe := &fakeProbe{err: errors.New("not publishing")}
	service := New(
		runner,
		probe,
		testConfig(),
	)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := service.Start(session.BroadcastId); err == nil {
		t.Fatal("Start succeeded, want ingest error")
	}

	current, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if current.Status != contracts.BroadcastSessionStatusFailed {
		t.Fatalf("status = %q, want failed", current.Status)
	}
	if len(probe.calls) != 1 || probe.calls[0] != session.BroadcastId {
		t.Fatalf("probe calls = %v, want [%s]", probe.calls, session.BroadcastId)
	}
	if got := runner.process(0).signalCount(); got != 1 {
		t.Fatalf("signals = %d, want 1 after a failed probe", got)
	}
}

func TestStartBuildsFFmpegArgs(t *testing.T) {
	runner := &fakeRunner{}
	service := newTestService(runner)
	spec := fileSpec()
	spec.Source.Loop = boolPointer(true)
	session, err := service.Create(spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	args := strings.Join(runner.call(0), " ")
	for _, want := range []string{
		"-stream_loop -1",
		"-i /tmp/fixture.mp4",
		"-c copy",
		"-f flv " + testRTMPBase + "/" + session.BroadcastId,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}

func TestCameraSourceUsesAVFoundation(t *testing.T) {
	runner := &fakeRunner{}
	service := newTestService(runner)
	session, err := service.Create(contracts.BroadcastCreateRequest{
		Source:       contracts.BroadcastSource{Type: contracts.Camera},
		LatencyClass: contracts.Low,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	args := strings.Join(runner.call(0), " ")
	for _, want := range []string{"-f avfoundation", "-i 0:0", "-c:v libx264", "-c:a aac"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}

func TestStopIsIdempotent(t *testing.T) {
	runner := &fakeRunner{}
	probe := &fakeProbe{}
	service := New(
		runner,
		probe,
		testConfig(),
	)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	first, err := service.Stop(session.BroadcastId)
	if err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	second, err := service.Stop(session.BroadcastId)
	if err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	if first.Status != contracts.BroadcastSessionStatusStopped || second.Status != contracts.BroadcastSessionStatusStopped {
		t.Fatalf("statuses = %q, %q, want stopped/stopped", first.Status, second.Status)
	}
	if first.StoppedAt == nil {
		t.Fatal("stoppedAt is nil after Stop")
	}
	if got := runner.process(0).signalCount(); got != 1 {
		t.Fatalf("signals = %d, want 1", got)
	}
	if runner.callCount() != 1 {
		t.Fatalf("ffmpeg starts = %d, want 1", runner.callCount())
	}
	if len(probe.unpublishCalls) != 1 || probe.unpublishCalls[0] != session.BroadcastId {
		t.Fatalf("unpublish calls = %v, want [%s]", probe.unpublishCalls, session.BroadcastId)
	}
}

func TestStopBeforeStart(t *testing.T) {
	service := newTestService(&fakeRunner{})
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	stopped, err := service.Stop(session.BroadcastId)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.Status != contracts.BroadcastSessionStatusStopped {
		t.Fatalf("status = %q, want stopped", stopped.Status)
	}
}

func TestProcessExitMarksFailed(t *testing.T) {
	runner := &fakeRunner{}
	service := newTestService(runner)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	runner.process(0).finish(errors.New("ffmpeg crashed"))

	waitForStatus(t, service, session.BroadcastId, contracts.BroadcastSessionStatusFailed)
}

func TestStartFailureMarksFailed(t *testing.T) {
	runner := &fakeRunner{err: errors.New("ffmpeg missing")}
	service := newTestService(runner)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := service.Start(session.BroadcastId); err == nil {
		t.Fatal("Start succeeded, want error")
	}

	current, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if current.Status != contracts.BroadcastSessionStatusFailed {
		t.Fatalf("status = %q, want failed", current.Status)
	}
}

func TestStartAfterStopLaunchesAgain(t *testing.T) {
	runner := &fakeRunner{}
	service := newTestService(runner)
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Stop(session.BroadcastId); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	restarted, err := service.Start(session.BroadcastId)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}

	if restarted.Status != contracts.BroadcastSessionStatusLive {
		t.Fatalf("status = %q, want live", restarted.Status)
	}
	if runner.callCount() != 2 {
		t.Fatalf("ffmpeg starts = %d, want 2", runner.callCount())
	}
}

func TestStatusReportsViewerCountWhileLive(t *testing.T) {
	probe := &fakeProbe{viewers: 3}
	service := New(&fakeRunner{}, probe, testConfig())
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	current, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if current.ViewerCount != 3 {
		t.Fatalf("viewerCount = %d, want 3", current.ViewerCount)
	}
	if len(probe.viewerCalls) != 1 || probe.viewerCalls[0] != session.BroadcastId {
		t.Fatalf("viewer calls = %v, want [%s]", probe.viewerCalls, session.BroadcastId)
	}
}

func TestStatusKeepsLastViewerCountWhenProbeFails(t *testing.T) {
	probe := &fakeProbe{viewers: 4}
	service := New(&fakeRunner{}, probe, testConfig())
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Status(session.BroadcastId); err != nil {
		t.Fatalf("Status: %v", err)
	}

	probe.viewerErr = errors.New("SRS API unavailable")
	current, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status after probe failure: %v", err)
	}
	if current.ViewerCount != 4 {
		t.Fatalf("viewerCount = %d, want the last known 4", current.ViewerCount)
	}
}

func TestStopClearsViewerCount(t *testing.T) {
	probe := &fakeProbe{viewers: 2}
	service := New(&fakeRunner{}, probe, testConfig())
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := service.Status(session.BroadcastId); err != nil {
		t.Fatalf("Status: %v", err)
	}

	stopped, err := service.Stop(session.BroadcastId)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.ViewerCount != 0 {
		t.Fatalf("viewerCount after stop = %d, want 0", stopped.ViewerCount)
	}
}

func TestStatusDoesNotPollViewersWhenNotLive(t *testing.T) {
	probe := &fakeProbe{viewers: 9}
	service := New(&fakeRunner{}, probe, testConfig())
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	idle, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if idle.ViewerCount != 0 {
		t.Fatalf("viewerCount while idle = %d, want 0", idle.ViewerCount)
	}
	if len(probe.viewerCalls) != 0 {
		t.Fatalf("viewer calls = %v, want none while idle", probe.viewerCalls)
	}
}

type blockingProbe struct {
	started chan struct{}
	release chan struct{}
	viewers int
}

func (p *blockingProbe) WaitForPublish(context.Context, string, time.Duration) error { return nil }

func (p *blockingProbe) EnsureUnpublished(context.Context, string, time.Duration) error { return nil }

func (p *blockingProbe) ViewerCount(context.Context, string) (int, error) {
	close(p.started)
	<-p.release
	return p.viewers, nil
}

func TestStatusDoesNotApplyViewerCountAfterStop(t *testing.T) {
	probe := &blockingProbe{
		started: make(chan struct{}),
		release: make(chan struct{}),
		viewers: 7,
	}
	service := New(&fakeRunner{}, probe, testConfig())
	session, err := service.Create(fileSpec())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Start(session.BroadcastId); err != nil {
		t.Fatalf("Start: %v", err)
	}

	type result struct {
		session contracts.BroadcastSession
		err     error
	}
	statuses := make(chan result, 1)
	go func() {
		current, err := service.Status(session.BroadcastId)
		statuses <- result{session: current, err: err}
	}()

	<-probe.started
	if _, err := service.Stop(session.BroadcastId); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(probe.release)

	first := <-statuses
	if first.err != nil {
		t.Fatalf("Status: %v", first.err)
	}
	if first.session.Status != contracts.BroadcastSessionStatusStopped {
		t.Fatalf("status = %q, want stopped", first.session.Status)
	}
	if first.session.ViewerCount != 0 {
		t.Fatalf("viewerCount = %d, want 0 after stop", first.session.ViewerCount)
	}

	current, err := service.Status(session.BroadcastId)
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if current.ViewerCount != 0 {
		t.Fatalf("viewerCount = %d, want 0", current.ViewerCount)
	}
}

func TestStatusUnknownBroadcast(t *testing.T) {
	service := newTestService(&fakeRunner{})

	if _, err := service.Status("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func waitForStatus(t *testing.T, service *Service, id string, want contracts.BroadcastSessionStatus) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, err := service.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if session.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	session, _ := service.Status(id)
	t.Fatalf("status = %q, want %q", session.Status, want)
}
