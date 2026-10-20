package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	roomCodeLength    = 6
	roomCodeAlphabet  = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	defaultRoomTTL    = 2 * time.Hour
	defaultWhipBase   = "/rtc/v1/whip/"
	defaultWhepBase   = "/rtc/v1/whep/"
	roomCommandBuffer = 64
	subscriberBuffer  = 32
)

var (
	ErrRoomNotFound       = errors.New("room not found")
	ErrRoomClosed         = errors.New("room is closed")
	ErrInvalidRoomRequest = errors.New("invalid room request")
	ErrForbidden          = errors.New("forbidden")
)

type RoomConfig struct {
	JoinSecret []byte
	WhipBase   string
	WhepBase   string
	TTL        time.Duration
}

// RoomSession owns the Meeting lifecycle: creation, joining, leaving, and the
// Roster. Each Room runs as an actor so join/leave/subscribe are serialized.
type RoomSession struct {
	mu       sync.Mutex
	rooms    map[string]*room
	signer   tokenSigner
	whipBase string
	whepBase string
	ttl      time.Duration
	now      func() time.Time
	newID    func() string
}

func NewRoomSession(config RoomConfig) (*RoomSession, error) {
	secret := append([]byte(nil), config.JoinSecret...)
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate join token secret: %w", err)
		}
	}

	if config.TTL <= 0 {
		config.TTL = defaultRoomTTL
	}
	if config.WhipBase == "" {
		config.WhipBase = defaultWhipBase
	}
	if config.WhepBase == "" {
		config.WhepBase = defaultWhepBase
	}

	return &RoomSession{
		rooms:    make(map[string]*room),
		signer:   tokenSigner{secret: secret},
		whipBase: config.WhipBase,
		whepBase: config.WhepBase,
		ttl:      config.TTL,
		now:      time.Now,
		newID:    randomID,
	}, nil
}

func (service *RoomSession) Create(request MeetingCreateRequest) (MeetingCreated, error) {
	if strings.TrimSpace(request.HostDisplayName) == "" {
		return MeetingCreated{}, ErrInvalidRoomRequest
	}

	now := service.now()
	expiresAt := now.Add(service.ttl)

	service.mu.Lock()
	defer service.mu.Unlock()

	code, err := service.uniqueRoomCodeLocked()
	if err != nil {
		return MeetingCreated{}, err
	}
	meetingID := service.newID()

	hostToken, err := service.signer.issue(joinClaims{
		RoomCode:  code,
		MeetingID: meetingID,
		Role:      Host,
		ExpiresAt: expiresAt.Unix(),
	})
	if err != nil {
		return MeetingCreated{}, err
	}

	created := &room{
		code:         code,
		meetingID:    meetingID,
		expiresAt:    expiresAt,
		whepBase:     service.whepBase,
		commands:     make(chan any, roomCommandBuffer),
		participants: make(map[string]*participant),
		subscribers:  make(map[string]*roomSubscriber),
	}
	service.rooms[code] = created
	// Read the clock dynamically so tests (and a future fake clock) can move it.
	go created.run(func() time.Time { return service.now() }, service.newID)

	return MeetingCreated{
		MeetingId: meetingID,
		RoomCode:  code,
		JoinToken: hostToken,
		ExpiresAt: expiresAt,
	}, nil
}

func (service *RoomSession) Join(
	roomCode string,
	request RoomJoinRequest,
	bearer string,
) (JoinGrant, error) {
	displayName := strings.TrimSpace(request.DisplayName)
	if displayName == "" {
		return JoinGrant{}, ErrInvalidRoomRequest
	}

	target, err := service.room(roomCode)
	if err != nil {
		return JoinGrant{}, err
	}

	role := Guest
	rejoinID := ""
	if bearer != "" {
		claims, err := service.verifyToken(bearer, roomCode)
		if err != nil {
			return JoinGrant{}, err
		}
		if claims.ParticipantID != "" {
			rejoinID = claims.ParticipantID
			role = claims.Role
		} else if claims.Role == Host {
			role = Host
		}
	}

	if request.ParticipantId != nil && *request.ParticipantId != "" {
		if rejoinID != *request.ParticipantId {
			return JoinGrant{}, ErrForbidden
		}
		rejoinID = *request.ParticipantId
	}

	reply := make(chan joinResult, 1)
	target.commands <- joinCommand{
		displayName: displayName,
		role:        role,
		rejoinID:    rejoinID,
		reply:       reply,
	}
	result := <-reply
	if result.err != nil {
		return JoinGrant{}, result.err
	}

	participantToken, err := service.signer.issue(joinClaims{
		RoomCode:      roomCode,
		MeetingID:     target.meetingID,
		ParticipantID: result.participantID,
		Role:          result.role,
		ExpiresAt:     target.expiresAt.Unix(),
	})
	if err != nil {
		return JoinGrant{}, err
	}

	return JoinGrant{
		ParticipantId: result.participantID,
		JoinToken:     participantToken,
		PublishUrl:    service.publishURL(result.participantID),
		Subscriptions: result.subscriptions,
	}, nil
}

// UpdateTracks replaces the set of Tracks a Participant reports publishing.
// The Room broadcasts the difference so other Participants can subscribe or
// release a WHEP session.
func (service *RoomSession) UpdateTracks(
	roomCode string,
	request RoomTrackUpdate,
	bearer string,
) error {
	target, err := service.room(roomCode)
	if err != nil {
		return err
	}

	claims, err := service.verifyParticipantToken(bearer, roomCode)
	if err != nil {
		return err
	}
	if claims.ParticipantID != request.ParticipantId {
		return ErrForbidden
	}

	tracks, err := normalizeTracks(request.Tracks)
	if err != nil {
		return err
	}

	reply := make(chan error, 1)
	target.commands <- tracksCommand{
		participantID: request.ParticipantId,
		tracks:        tracks,
		reply:         reply,
	}
	return <-reply
}

// UpdateMute updates the Participant's microphone mute state. Mute is
// Participant state rather than Track state: a muted Participant keeps
// publishing the microphone Track and the browser sends silence.
func (service *RoomSession) UpdateMute(
	roomCode string,
	request RoomMuteUpdate,
	bearer string,
) error {
	target, err := service.room(roomCode)
	if err != nil {
		return err
	}

	claims, err := service.verifyParticipantToken(bearer, roomCode)
	if err != nil {
		return err
	}
	if claims.ParticipantID != request.ParticipantId {
		return ErrForbidden
	}

	reply := make(chan error, 1)
	target.commands <- muteCommand{
		participantID: request.ParticipantId,
		muted:         request.Muted,
		reply:         reply,
	}
	return <-reply
}

// normalizeTracks validates the reported set and converts it to the Roster's
// summary shape. Every reported Track is published by definition: to stop
// publishing one, the caller reports the set without it.
func normalizeTracks(tracks []RoomTrack) ([]TrackSummary, error) {
	normalized := make([]TrackSummary, 0, len(tracks))
	seen := make(map[string]struct{}, len(tracks))
	for _, track := range tracks {
		if strings.TrimSpace(track.TrackId) == "" || !track.Kind.Valid() {
			return nil, ErrInvalidRoomRequest
		}
		if _, duplicate := seen[track.TrackId]; duplicate {
			return nil, ErrInvalidRoomRequest
		}
		seen[track.TrackId] = struct{}{}

		published := true
		normalized = append(normalized, TrackSummary{
			TrackId:   track.TrackId,
			Kind:      TrackSummaryKind(track.Kind),
			Published: &published,
		})
	}
	return normalized, nil
}

func (service *RoomSession) Leave(roomCode, participantID, bearer string) error {
	target, err := service.room(roomCode)
	if err != nil {
		return err
	}

	claims, err := service.verifyParticipantToken(bearer, roomCode)
	if err != nil {
		return err
	}
	if claims.ParticipantID != participantID {
		return ErrForbidden
	}

	reply := make(chan error, 1)
	target.commands <- leaveCommand{participantID: participantID, reply: reply}
	return <-reply
}

func (service *RoomSession) Status(roomCode, bearer string) (RoomStatus, error) {
	target, err := service.room(roomCode)
	if err != nil {
		return RoomStatus{}, err
	}
	if _, err := service.verifyParticipantToken(bearer, roomCode); err != nil {
		return RoomStatus{}, err
	}

	reply := make(chan RoomStatus, 1)
	target.commands <- statusCommand{reply: reply}
	return <-reply, nil
}

// Subscribe authenticates a participant and registers their event stream.
func (service *RoomSession) Subscribe(roomCode, bearer string) (*roomSubscriber, error) {
	claims, err := service.verifyParticipantToken(bearer, roomCode)
	if err != nil {
		return nil, err
	}

	target, err := service.room(roomCode)
	if err != nil {
		return nil, err
	}

	subscriber := &roomSubscriber{
		participantID: claims.ParticipantID,
		events:        make(chan any, subscriberBuffer),
		done:          make(chan struct{}),
	}
	reply := make(chan error, 1)
	target.commands <- subscribeCommand{subscriber: subscriber, reply: reply}
	if err := <-reply; err != nil {
		return nil, err
	}
	return subscriber, nil
}

func (service *RoomSession) Unsubscribe(roomCode string, subscriber *roomSubscriber) {
	if subscriber == nil {
		return
	}

	target, err := service.room(roomCode)
	if err != nil {
		return
	}
	// The actor never blocks, so a synchronous enqueue is safe and ensures a
	// disconnected socket does not leave a dead subscriber behind.
	target.commands <- unsubscribeCommand{subscriber: subscriber}
}

func (service *RoomSession) room(roomCode string) (*room, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	target, ok := service.rooms[roomCode]
	if !ok {
		return nil, ErrRoomNotFound
	}
	return target, nil
}

func (service *RoomSession) verifyToken(raw, roomCode string) (joinClaims, error) {
	if raw == "" {
		return joinClaims{}, ErrInvalidJoinToken
	}

	claims, err := service.signer.verify(raw, service.now())
	if err != nil {
		return joinClaims{}, err
	}
	if claims.RoomCode != roomCode {
		return joinClaims{}, ErrInvalidJoinToken
	}
	return claims, nil
}

func (service *RoomSession) verifyParticipantToken(raw, roomCode string) (joinClaims, error) {
	claims, err := service.verifyToken(raw, roomCode)
	if err != nil {
		return joinClaims{}, err
	}
	if claims.ParticipantID == "" {
		return joinClaims{}, ErrForbidden
	}
	return claims, nil
}

func (service *RoomSession) publishURL(participantID string) string {
	return streamURL(service.whipBase, participantID)
}

// streamURL appends the app/stream query SRS uses to address one Stream. The
// base may be relative: the browser resolves it against the app origin, and
// Vite or Caddy proxies /rtc to SRS.
func streamURL(base, participantID string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}

	query := parsed.Query()
	query.Set("app", "live")
	query.Set("stream", participantID)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (service *RoomSession) uniqueRoomCodeLocked() (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := randomRoomCode()
		if _, exists := service.rooms[code]; !exists {
			return code, nil
		}
	}
	return "", errors.New("could not allocate a unique room code")
}

func randomRoomCode() string {
	var raw [roomCodeLength]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1_000_000)
	}

	code := make([]byte, roomCodeLength)
	for index, value := range raw {
		code[index] = roomCodeAlphabet[int(value)%len(roomCodeAlphabet)]
	}
	return string(code)
}
