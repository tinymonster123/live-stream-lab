package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	stopTimeout      = 5 * time.Second
	ingestTimeout    = 5 * time.Second
	unpublishTimeout = 2 * time.Second
)

var (
	ErrNotFound           = errors.New("broadcast not found")
	ErrInvalidBroadcast   = errors.New("invalid broadcast")
	ErrUnsupportedLatency = errors.New("unsupported latency class")
)

type ServiceConfig struct {
	RTMPBase string
	FLVBase  string
}

type broadcastSession struct {
	mu           sync.Mutex
	id           string
	source       BroadcastSource
	latencyClass LatencyClass
	status       BroadcastSessionStatus
	publish      PublishDescriptor
	playback     PlaybackDescriptor
	process      Process
	startedAt    *time.Time
	stoppedAt    *time.Time
}

func (session *broadcastSession) snapshotLocked() BroadcastSession {
	snapshot := BroadcastSession{
		BroadcastId:  session.id,
		Status:       session.status,
		LatencyClass: session.latencyClass,
		Publish:      session.publish,
		Playback:     session.playback,
		ViewerCount:  0,
	}
	if session.startedAt != nil {
		startedAt := *session.startedAt
		snapshot.StartedAt = &startedAt
	}
	if session.stoppedAt != nil {
		stoppedAt := *session.stoppedAt
		snapshot.StoppedAt = &stoppedAt
	}
	return snapshot
}

type BroadcastService struct {
	mu       sync.Mutex
	sessions map[string]*broadcastSession
	runner   ProcessRunner
	probe    IngestProbe
	config   ServiceConfig
	now      func() time.Time
	newID    func() string
}

func NewBroadcastService(runner ProcessRunner, probe IngestProbe, config ServiceConfig) *BroadcastService {
	return &BroadcastService{
		sessions: make(map[string]*broadcastSession),
		runner:   runner,
		probe:    probe,
		config:   config,
		now:      time.Now,
		newID:    randomID,
	}
}

func (service *BroadcastService) Create(spec BroadcastCreateRequest) (BroadcastSession, error) {
	if err := validateSpec(spec); err != nil {
		return BroadcastSession{}, err
	}

	id := service.newID()
	playback, err := resolvePlayback(id, spec.LatencyClass, service.config.FLVBase, service.now())
	if err != nil {
		return BroadcastSession{}, err
	}

	session := &broadcastSession{
		id:           id,
		source:       spec.Source,
		latencyClass: spec.LatencyClass,
		status:       BroadcastSessionStatusIdle,
		publish: PublishDescriptor{
			RtmpUrl:   service.config.RTMPBase,
			StreamKey: id,
		},
		playback: playback,
	}

	service.mu.Lock()
	service.sessions[id] = session
	service.mu.Unlock()

	session.mu.Lock()
	defer session.mu.Unlock()
	return session.snapshotLocked(), nil
}

func (service *BroadcastService) Start(id string) (BroadcastSession, error) {
	session, err := service.session(id)
	if err != nil {
		return BroadcastSession{}, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.status == BroadcastSessionStatusLive || session.status == BroadcastSessionStatusStarting {
		return session.snapshotLocked(), nil
	}

	target := joinURL(service.config.RTMPBase, session.id)
	process, err := service.runner.Start(ffmpegArgs(session.source, target))
	if err != nil {
		session.status = BroadcastSessionStatusFailed
		stoppedAt := service.now()
		session.stoppedAt = &stoppedAt
		return BroadcastSession{}, fmt.Errorf("start ffmpeg: %w", err)
	}

	startedAt := service.now()
	session.process = process
	session.status = BroadcastSessionStatusStarting
	session.startedAt = &startedAt
	session.stoppedAt = nil

	if service.probe != nil {
		if err := service.probe.WaitForPublish(context.Background(), session.id, ingestTimeout); err != nil {
			stopProcess(process, stopTimeout)
			session.process = nil
			session.status = BroadcastSessionStatusFailed
			stoppedAt := service.now()
			session.stoppedAt = &stoppedAt
			return BroadcastSession{}, fmt.Errorf("wait for SRS ingest: %w", err)
		}
	}

	session.status = BroadcastSessionStatusLive
	snapshot := session.snapshotLocked()

	go service.watch(session, process)
	return snapshot, nil
}

func (service *BroadcastService) Stop(id string) (BroadcastSession, error) {
	session, err := service.session(id)
	if err != nil {
		return BroadcastSession{}, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.status == BroadcastSessionStatusStopped {
		return session.snapshotLocked(), nil
	}

	if session.process != nil {
		stopProcess(session.process, stopTimeout)
		session.process = nil
	}

	if service.probe != nil {
		if err := service.probe.EnsureUnpublished(
			context.Background(),
			session.id,
			unpublishTimeout,
		); err != nil {
			log.Printf("broadcast %s: ensure unpublished: %v", session.id, err)
		}
	}

	stoppedAt := service.now()
	session.status = BroadcastSessionStatusStopped
	session.stoppedAt = &stoppedAt
	return session.snapshotLocked(), nil
}

func (service *BroadcastService) Status(id string) (BroadcastSession, error) {
	session, err := service.session(id)
	if err != nil {
		return BroadcastSession{}, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	return session.snapshotLocked(), nil
}

func (service *BroadcastService) session(id string) (*broadcastSession, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	session, ok := service.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return session, nil
}

func (service *BroadcastService) watch(session *broadcastSession, process Process) {
	<-process.Done()

	session.mu.Lock()
	defer session.mu.Unlock()

	if session.process != process {
		return
	}

	session.process = nil
	if session.status == BroadcastSessionStatusStopping ||
		session.status == BroadcastSessionStatusStopped {
		return
	}

	log.Printf("broadcast %s: ffmpeg exited: %v", session.id, process.Err())
	session.status = BroadcastSessionStatusFailed
	stoppedAt := service.now()
	session.stoppedAt = &stoppedAt
}

func validateSpec(spec BroadcastCreateRequest) error {
	switch spec.Source.Type {
	case File:
		if stringValue(spec.Source.Path) == "" {
			return fmt.Errorf("%w: source.path is required for file sources", ErrInvalidBroadcast)
		}
	case Camera:
	default:
		return fmt.Errorf("%w: source.type must be file or camera", ErrInvalidBroadcast)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func randomID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}
