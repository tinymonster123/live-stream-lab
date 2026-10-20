package main

import (
	"log"
	"time"
)

type room struct {
	code      string
	meetingID string
	expiresAt time.Time
	whepBase  string
	commands  chan any

	participants map[string]*participant
	subscribers  map[string]*roomSubscriber
	order        []string
}

type participant struct {
	id          string
	displayName string
	role        RosterEntryRole
	muted       bool
	tracks      []TrackSummary
}

type roomSubscriber struct {
	participantID string
	events        chan any
	done          chan struct{}
}

type joinCommand struct {
	displayName string
	role        RosterEntryRole
	rejoinID    string
	reply       chan joinResult
}

type joinResult struct {
	participantID string
	displayName   string
	role          RosterEntryRole
	subscriptions []SubscriptionTarget
	err           error
}

type leaveCommand struct {
	participantID string
	reply         chan error
}

type statusCommand struct {
	reply chan RoomStatus
}

type subscribeCommand struct {
	subscriber *roomSubscriber
	reply      chan error
}

type unsubscribeCommand struct {
	subscriber *roomSubscriber
}

type tracksCommand struct {
	participantID string
	tracks        []TrackSummary
	reply         chan error
}

type muteCommand struct {
	participantID string
	muted         bool
	reply         chan error
}

func (room *room) run(now func() time.Time, newID func() string) {
	for command := range room.commands {
		switch command := command.(type) {
		case joinCommand:
			command.reply <- room.join(command, now(), newID)
		case leaveCommand:
			command.reply <- room.leave(command)
		case statusCommand:
			command.reply <- room.status(now())
		case subscribeCommand:
			command.reply <- room.subscribe(command, now())
		case unsubscribeCommand:
			room.unsubscribe(command)
		case tracksCommand:
			command.reply <- room.updateTracks(command)
		case muteCommand:
			command.reply <- room.updateMute(command)
		}
	}
}

func (room *room) join(command joinCommand, now time.Time, newID func() string) joinResult {
	if !now.Before(room.expiresAt) {
		return joinResult{err: ErrRoomClosed}
	}

	participantID := ""
	if command.rejoinID != "" {
		if existing, ok := room.participants[command.rejoinID]; ok {
			existing.displayName = command.displayName
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
	if role != Host && role != Guest {
		role = Guest
	}
	if role == Host && room.hasHost() {
		role = Guest
	}

	if participantID == "" {
		participantID = newID()
	}
	participant := &participant{
		id:          participantID,
		displayName: command.displayName,
		role:        role,
	}
	room.participants[participant.id] = participant
	room.order = append(room.order, participant.id)
	room.broadcast(joinedEvent(participant), "")

	return joinResult{
		participantID: participant.id,
		displayName:   participant.displayName,
		role:          participant.role,
		subscriptions: room.subscriptionsFor(participant.id),
	}
}

func (room *room) leave(command leaveCommand) error {
	if _, ok := room.participants[command.participantID]; !ok {
		return nil
	}

	delete(room.participants, command.participantID)
	room.removeFromOrder(command.participantID)
	room.broadcast(ParticipantLeftEvent{
		Type:          "participant.left",
		ParticipantId: command.participantID,
	}, command.participantID)

	if subscriber, ok := room.subscribers[command.participantID]; ok {
		close(subscriber.done)
		delete(room.subscribers, command.participantID)
	}
	return nil
}

func (room *room) status(now time.Time) RoomStatus {
	state := Open
	if !now.Before(room.expiresAt) {
		state = Closed
	}

	participants := make([]RosterEntry, 0, len(room.participants))
	for _, id := range room.order {
		participant, ok := room.participants[id]
		if !ok {
			continue
		}
		tracks := make([]TrackSummary, len(participant.tracks))
		copy(tracks, participant.tracks)
		participants = append(participants, RosterEntry{
			ParticipantId: participant.id,
			DisplayName:   participant.displayName,
			Role:          participant.role,
			Muted:         participant.muted,
			Tracks:        tracks,
		})
	}

	return RoomStatus{
		RoomCode:     room.code,
		State:        state,
		Participants: participants,
	}
}

func (room *room) subscribe(command subscribeCommand, now time.Time) error {
	if !now.Before(room.expiresAt) {
		return ErrRoomClosed
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

func (room *room) updateTracks(command tracksCommand) error {
	participant, ok := room.participants[command.participantID]
	if !ok {
		return ErrForbidden
	}

	previous := make(map[string]TrackSummary, len(participant.tracks))
	for _, track := range participant.tracks {
		previous[track.TrackId] = track
	}

	next := make(map[string]TrackSummary, len(command.tracks))
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

func (room *room) updateMute(command muteCommand) error {
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

func (room *room) unpublish(participantID string, track TrackSummary) {
	room.broadcast(TrackUnpublishedEvent{
		Type:          "track.unpublished",
		ParticipantId: participantID,
		TrackId:       track.TrackId,
	}, participantID)
}

// subscriptionsFor lists one WHEP target per published Track of every other
// Participant. Tracks from the same Participant share a Stream and WHEP URL,
// so callers deduplicate by Participant before opening peer connections.
func (room *room) subscriptionsFor(participantID string) []SubscriptionTarget {
	subscriptions := []SubscriptionTarget{}
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
			subscriptions = append(subscriptions, SubscriptionTarget{
				ParticipantId: participant.id,
				TrackId:       track.TrackId,
				WhepUrl:       whepURL,
			})
		}
	}
	return subscriptions
}

func (room *room) publishedEvent(participantID string, track TrackSummary) TrackPublishedEvent {
	return TrackPublishedEvent{
		Type:          "track.published",
		ParticipantId: participantID,
		TrackId:       track.TrackId,
		Kind:          TrackPublishedEventKind(track.Kind),
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
		if participant.role == Host {
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

func joinedEvent(participant *participant) ParticipantJoinedEvent {
	return ParticipantJoinedEvent{
		Type:          "participant.joined",
		ParticipantId: participant.id,
		DisplayName:   participant.displayName,
		Role:          participant.role,
	}
}

func muteChangedEvent(participantID string, muted bool) MuteChangedEvent {
	return MuteChangedEvent{
		Type:          "mute.changed",
		ParticipantId: participantID,
		Muted:         muted,
	}
}
