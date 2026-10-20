package room

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

type room struct {
	code      string
	meetingID string
	expiresAt time.Time
	whepBase  string
	commands  chan any

	// heartbeatTimeout and emptyGrace are copied from the Session config so
	// a Room can run with shorter timers in tests.
	heartbeatTimeout time.Duration
	emptyGrace       time.Duration

	participants map[string]*participant
	subscribers  map[string]*Subscriber
	order        []string

	// emptySince is when the Room last had no online Participant; zero while
	// at least one Participant is online.
	emptySince time.Time
	// kicked blocks a participantId from rejoining after the host removed it.
	kicked map[string]struct{}
	// closed is read by the session ticker to skip tombstoned Rooms.
	closed atomic.Bool
}

type participant struct {
	id          string
	displayName string
	role        contracts.RosterEntryRole
	muted       bool
	tracks      []contracts.TrackSummary
	lastSeen    time.Time
	online      bool
}

// Subscriber is one Participant's signaling stream. The transport layer writes
// its events to a WebSocket and closes the socket when Done closes.
type Subscriber struct {
	participantID string
	events        chan any
	done          chan struct{}
	// closeReason is set when the Room removes the Participant (a kick) so the
	// socket handler can tell the client why it is being disconnected.
	closeReason string
}

func (subscriber *Subscriber) ParticipantID() string { return subscriber.participantID }

func (subscriber *Subscriber) Events() <-chan any { return subscriber.events }

func (subscriber *Subscriber) Done() <-chan struct{} { return subscriber.done }

// CloseReason is set before Done closes when the Room removed the Participant.
// It is empty when the socket ended for any other reason.
func (subscriber *Subscriber) CloseReason() string { return subscriber.closeReason }

type joinCommand struct {
	displayName string
	role        contracts.RosterEntryRole
	rejoinID    string
	reply       chan joinResult
}

type joinResult struct {
	participantID string
	displayName   string
	role          contracts.RosterEntryRole
	subscriptions []contracts.SubscriptionTarget
	err           error
}

type leaveCommand struct {
	participantID string
	reply         chan error
}

type statusCommand struct {
	participantID string
	reply         chan statusResult
}

type statusResult struct {
	status contracts.RoomStatus
	err    error
}

type subscribeCommand struct {
	subscriber *Subscriber
	reply      chan error
}

type unsubscribeCommand struct {
	subscriber *Subscriber
}

type tracksCommand struct {
	participantID string
	tracks        []contracts.TrackSummary
	reply         chan error
}

type muteCommand struct {
	participantID string
	muted         bool
	reply         chan error
}

type heartbeatCommand struct {
	participantID string
	reply         chan error
}

type kickCommand struct {
	hostID        string
	participantID string
	reply         chan error
}

type sweepCommand struct {
	now   time.Time
	reply chan struct{}
}

func (room *room) run(now func() time.Time, newID func() string) {
	for command := range room.commands {
		switch command := command.(type) {
		case joinCommand:
			command.reply <- room.join(command, now(), newID)
		case leaveCommand:
			command.reply <- room.leave(command, now())
		case statusCommand:
			command.reply <- room.status(command, now())
		case subscribeCommand:
			command.reply <- room.subscribe(command, now())
		case unsubscribeCommand:
			room.unsubscribe(command)
		case tracksCommand:
			command.reply <- room.updateTracks(command, now())
		case muteCommand:
			command.reply <- room.updateMute(command, now())
		case heartbeatCommand:
			command.reply <- room.heartbeat(command.participantID, now())
		case kickCommand:
			command.reply <- room.kick(command, now())
		case sweepCommand:
			room.sweep(command.now)
			close(command.reply)
		}
	}
}

// inactive reports whether the Room refuses further state changes: it closed
// through the empty grace period or reached its TTL.
func (room *room) inactive(now time.Time) bool {
	return room.closed.Load() || !now.Before(room.expiresAt)
}

// sweep advances the Room's time-based state: heartbeat timeouts, the empty
// grace period, and the overall TTL.
func (room *room) sweep(now time.Time) {
	if room.closed.Load() {
		return
	}
	if !now.Before(room.expiresAt) {
		room.close()
		return
	}

	for _, id := range room.order {
		participant, ok := room.participants[id]
		if !ok || !participant.online || now.Sub(participant.lastSeen) < room.heartbeatTimeout {
			continue
		}
		participant.online = false
		room.broadcast(contracts.ParticipantOfflineEvent{
			Type:          "participant.offline",
			ParticipantId: participant.id,
		}, "")
	}

	room.refreshEmpty(now)
	if !room.emptySince.IsZero() && now.Sub(room.emptySince) >= room.emptyGrace {
		room.close()
	}
}

// refreshEmpty starts or clears the empty-room timer. The Room is empty while
// no Participant is online: participants that dropped or locked their phone
// keep their Roster entry but stop holding the Room open.
func (room *room) refreshEmpty(now time.Time) {
	for _, participant := range room.participants {
		if participant.online {
			room.emptySince = time.Time{}
			return
		}
	}
	if room.emptySince.IsZero() {
		room.emptySince = now
	}
}

func (room *room) close() {
	room.closed.Store(true)
	for participantID, subscriber := range room.subscribers {
		close(subscriber.done)
		delete(room.subscribers, participantID)
	}
}

func (room *room) join(command joinCommand, now time.Time, newID func() string) joinResult {
	if room.inactive(now) {
		return joinResult{err: ErrClosed}
	}

	participantID := ""
	if command.rejoinID != "" {
		if _, banned := room.kicked[command.rejoinID]; banned {
			return joinResult{err: ErrForbidden}
		}
		if existing, ok := room.participants[command.rejoinID]; ok {
			existing.displayName = command.displayName
			existing.lastSeen = now
			existing.online = true
			room.refreshEmpty(now)
			room.broadcast(joinedEvent(existing), "")
			// A rejoin starts a new media session, so Tracks and mute state from
			// the dropped session no longer describe anything. Subscribers must
			// release their WHEP connections until the Participant publishes
			// again, and the client captures fresh devices unmuted.
			room.clearTracks(existing)
			room.clearMute(existing)
			return joinResult{
				participantID: existing.id,
				displayName:   existing.displayName,
				role:          existing.role,
				subscriptions: room.subscriptionsFor(existing.id),
			}
		}

		// The Participant left the Room but still holds a token for this
		// identity. Recreate the Roster entry with the same id so other
		// Participants and the client keep the id they already know.
		participantID = command.rejoinID
	}

	role := command.role
	if role != contracts.Host && role != contracts.Guest {
		role = contracts.Guest
	}
	if role == contracts.Host && room.hasHost() {
		role = contracts.Guest
	}

	if participantID == "" {
		participantID = newID()
	}
	participant := &participant{
		id:          participantID,
		displayName: command.displayName,
		role:        role,
		lastSeen:    now,
		online:      true,
	}
	room.participants[participant.id] = participant
	room.order = append(room.order, participant.id)
	room.refreshEmpty(now)
	room.broadcast(joinedEvent(participant), "")

	return joinResult{
		participantID: participant.id,
		displayName:   participant.displayName,
		role:          participant.role,
		subscriptions: room.subscriptionsFor(participant.id),
	}
}

func (room *room) leave(command leaveCommand, now time.Time) error {
	room.removeParticipant(command.participantID, now, "")
	return nil
}

func (room *room) kick(command kickCommand, now time.Time) error {
	if room.inactive(now) {
		return ErrClosed
	}
	host, ok := room.participants[command.hostID]
	if !ok || host.role != contracts.Host {
		return ErrForbidden
	}
	if command.participantID == "" || command.participantID == command.hostID {
		return ErrInvalidRequest
	}
	if _, ok := room.participants[command.participantID]; !ok {
		// Already gone: a retried kick is a no-op, and an id that never joined
		// must not grow the deny list.
		return nil
	}

	room.kicked[command.participantID] = struct{}{}
	room.removeParticipant(command.participantID, now, "kicked")
	return nil
}

// removeParticipant drops a Participant from the Roster, tells the other
// Participants, and closes the removed Participant's signaling socket. The
// closeReason distinguishes a kick from an ordinary leave.
func (room *room) removeParticipant(participantID string, now time.Time, closeReason string) {
	if _, ok := room.participants[participantID]; !ok {
		return
	}

	delete(room.participants, participantID)
	room.removeFromOrder(participantID)
	room.broadcast(contracts.ParticipantLeftEvent{
		Type:          "participant.left",
		ParticipantId: participantID,
	}, participantID)

	if subscriber, ok := room.subscribers[participantID]; ok {
		subscriber.closeReason = closeReason
		close(subscriber.done)
		delete(room.subscribers, participantID)
	}

	room.refreshEmpty(now)
}

func (room *room) heartbeat(participantID string, now time.Time) error {
	if room.inactive(now) {
		return ErrClosed
	}
	participant, ok := room.participants[participantID]
	if !ok {
		return ErrForbidden
	}

	participant.lastSeen = now
	if participant.online {
		return nil
	}

	participant.online = true
	room.refreshEmpty(now)
	room.broadcast(contracts.ParticipantOnlineEvent{
		Type:          "participant.online",
		ParticipantId: participant.id,
	}, "")
	return nil
}

func (room *room) status(command statusCommand, now time.Time) statusResult {
	// Only current Participants may read the Roster; a kicked or departed
	// Participant's still-valid token gets no access.
	if _, ok := room.participants[command.participantID]; !ok {
		return statusResult{err: ErrForbidden}
	}

	state := contracts.Open
	switch {
	case room.inactive(now):
		state = contracts.Closed
	case !room.emptySince.IsZero():
		state = contracts.Closing
	}

	participants := make([]contracts.RosterEntry, 0, len(room.participants))
	for _, id := range room.order {
		participant, ok := room.participants[id]
		if !ok {
			continue
		}
		tracks := make([]contracts.TrackSummary, len(participant.tracks))
		copy(tracks, participant.tracks)
		participants = append(participants, contracts.RosterEntry{
			ParticipantId: participant.id,
			DisplayName:   participant.displayName,
			Role:          participant.role,
			Online:        participant.online,
			Muted:         participant.muted,
			Tracks:        tracks,
		})
	}

	return statusResult{status: contracts.RoomStatus{
		RoomCode:     room.code,
		State:        state,
		Participants: participants,
	}}
}

func (room *room) subscribe(command subscribeCommand, now time.Time) error {
	if room.inactive(now) {
		return ErrClosed
	}
	if _, ok := room.participants[command.subscriber.participantID]; !ok {
		return ErrForbidden
	}

	if existing, ok := room.subscribers[command.subscriber.participantID]; ok {
		close(existing.done)
	}
	room.subscribers[command.subscriber.participantID] = command.subscriber

	// Replay the current Roster to the new subscriber so events between the
	// initial status read and the socket upgrade are not missed.
	for _, id := range room.order {
		if id == command.subscriber.participantID {
			continue
		}
		participant, ok := room.participants[id]
		if !ok {
			continue
		}
		select {
		case command.subscriber.events <- joinedEvent(participant):
		default:
			log.Printf("room %s: dropping roster snapshot for %s", room.code, command.subscriber.participantID)
			continue
		}

		// joinedEvent implies presence; replay an offline Participant's state so
		// a late subscriber sees the same Roster as a live one.
		if !participant.online {
			select {
			case command.subscriber.events <- contracts.ParticipantOfflineEvent{
				Type:          "participant.offline",
				ParticipantId: participant.id,
			}:
			default:
				log.Printf("room %s: dropping presence snapshot for %s", room.code, command.subscriber.participantID)
			}
		}

		// Mute state arrived after the Participant joined; replay it before the
		// Tracks so a late subscriber sees the same state as a live one.
		if participant.muted {
			select {
			case command.subscriber.events <- muteChangedEvent(participant.id, true):
			default:
				log.Printf("room %s: dropping mute snapshot for %s", room.code, command.subscriber.participantID)
			}
		}

		// Track state arrived after the Participant joined, so replay it next;
		// otherwise a subscriber connecting now would miss every WHEP target.
		for _, track := range participant.tracks {
			select {
			case command.subscriber.events <- room.publishedEvent(participant.id, track):
			default:
				log.Printf("room %s: dropping track snapshot for %s", room.code, command.subscriber.participantID)
			}
		}
	}
	return nil
}

func (room *room) unsubscribe(command unsubscribeCommand) {
	if room.subscribers[command.subscriber.participantID] == command.subscriber {
		delete(room.subscribers, command.subscriber.participantID)
	}
}

func (room *room) updateTracks(command tracksCommand, now time.Time) error {
	if room.inactive(now) {
		return ErrClosed
	}
	participant, ok := room.participants[command.participantID]
	if !ok {
		return ErrForbidden
	}

	previous := make(map[string]contracts.TrackSummary, len(participant.tracks))
	for _, track := range participant.tracks {
		previous[track.TrackId] = track
	}

	next := make(map[string]contracts.TrackSummary, len(command.tracks))
	for _, track := range command.tracks {
		next[track.TrackId] = track
		if old, ok := previous[track.TrackId]; !ok || old.Kind != track.Kind {
			room.broadcast(room.publishedEvent(participant.id, track), participant.id)
		}
	}
	for _, track := range participant.tracks {
		if _, ok := next[track.TrackId]; !ok {
			room.unpublish(participant.id, track)
		}
	}

	participant.tracks = command.tracks
	return nil
}

func (room *room) updateMute(command muteCommand, now time.Time) error {
	if room.inactive(now) {
		return ErrClosed
	}
	participant, ok := room.participants[command.participantID]
	if !ok {
		return ErrForbidden
	}
	if participant.muted == command.muted {
		return nil
	}

	participant.muted = command.muted
	room.broadcast(muteChangedEvent(participant.id, command.muted), participant.id)
	return nil
}

func (room *room) clearMute(participant *participant) {
	if !participant.muted {
		return
	}
	participant.muted = false
	room.broadcast(muteChangedEvent(participant.id, false), participant.id)
}

func (room *room) clearTracks(participant *participant) {
	for _, track := range participant.tracks {
		room.unpublish(participant.id, track)
	}
	participant.tracks = nil
}

func (room *room) unpublish(participantID string, track contracts.TrackSummary) {
	room.broadcast(contracts.TrackUnpublishedEvent{
		Type:          "track.unpublished",
		ParticipantId: participantID,
		TrackId:       track.TrackId,
	}, participantID)
}

// subscriptionsFor lists one WHEP target per published Track of every other
// Participant. Tracks from the same Participant share a Stream and WHEP URL,
// so callers deduplicate by Participant before opening peer connections.
func (room *room) subscriptionsFor(participantID string) []contracts.SubscriptionTarget {
	subscriptions := []contracts.SubscriptionTarget{}
	for _, id := range room.order {
		if id == participantID {
			continue
		}
		participant, ok := room.participants[id]
		if !ok {
			continue
		}
		whepURL := room.whepURL(participant.id)
		for _, track := range participant.tracks {
			subscriptions = append(subscriptions, contracts.SubscriptionTarget{
				ParticipantId: participant.id,
				TrackId:       track.TrackId,
				WhepUrl:       whepURL,
			})
		}
	}
	return subscriptions
}

func (room *room) publishedEvent(participantID string, track contracts.TrackSummary) contracts.TrackPublishedEvent {
	return contracts.TrackPublishedEvent{
		Type:          "track.published",
		ParticipantId: participantID,
		TrackId:       track.TrackId,
		Kind:          contracts.TrackPublishedEventKind(track.Kind),
		WhepUrl:       room.whepURL(participantID),
	}
}

func (room *room) whepURL(participantID string) string {
	return streamURL(room.whepBase, participantID)
}

func (room *room) broadcast(event any, exceptParticipant string) {
	for participantID, subscriber := range room.subscribers {
		if participantID == exceptParticipant {
			continue
		}
		select {
		case subscriber.events <- event:
		default:
			log.Printf("room %s: dropping event for %s", room.code, participantID)
		}
	}
}

func (room *room) hasHost() bool {
	for _, participant := range room.participants {
		if participant.role == contracts.Host {
			return true
		}
	}
	return false
}

func (room *room) removeFromOrder(participantID string) {
	for index, id := range room.order {
		if id == participantID {
			room.order = append(room.order[:index], room.order[index+1:]...)
			return
		}
	}
}

func joinedEvent(participant *participant) contracts.ParticipantJoinedEvent {
	return contracts.ParticipantJoinedEvent{
		Type:          "participant.joined",
		ParticipantId: participant.id,
		DisplayName:   participant.displayName,
		Role:          participant.role,
	}
}

func muteChangedEvent(participantID string, muted bool) contracts.MuteChangedEvent {
	return contracts.MuteChangedEvent{
		Type:          "mute.changed",
		ParticipantId: participantID,
		Muted:         muted,
	}
}
