package main

import (
	"log"
	"time"
)

type room struct {
	code      string
	meetingID string
	expiresAt time.Time
	commands  chan any

	participants map[string]*participant
	subscribers  map[string]*roomSubscriber
	order        []string
}

type participant struct {
	id          string
	displayName string
	role        RosterEntryRole
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
		}
	}
}

func (room *room) join(command joinCommand, now time.Time, newID func() string) joinResult {
	if !now.Before(room.expiresAt) {
		return joinResult{err: ErrRoomClosed}
	}

	if command.rejoinID != "" {
		if existing, ok := room.participants[command.rejoinID]; ok {
			existing.displayName = command.displayName
			room.broadcast(joinedEvent(existing), "")
			return joinResult{
				participantID: existing.id,
				displayName:   existing.displayName,
				role:          existing.role,
			}
		}
	}

	role := command.role
	if role != Host && role != Guest {
		role = Guest
	}
	if role == Host && room.hasHost() {
		role = Guest
	}

	participant := &participant{
		id:          newID(),
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
		participants = append(participants, RosterEntry{
			ParticipantId: participant.id,
			DisplayName:   participant.displayName,
			Role:          participant.role,
			Tracks:        []TrackSummary{},
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
		}
	}
	return nil
}

func (room *room) unsubscribe(command unsubscribeCommand) {
	if room.subscribers[command.subscriber.participantID] == command.subscriber {
		delete(room.subscribers, command.subscriber.participantID)
	}
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
