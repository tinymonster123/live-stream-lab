# live-stream-lab

Local live meeting and broadcast system. SRS is the media plane; FFmpeg captures, records, and processes; the Go control plane owns rooms, lifecycle, and access.

## Language

### Live sessions

**Broadcast**:
A one-way live stream from one publisher to viewers. M1.
_Avoid_: Stream, live stream (when you mean the product session)

**Meeting**:
A two-way live session where Participants publish and subscribe to each other's Tracks. M2+.
_Avoid_: Call, conference

**Room**:
The live media container of a Meeting. It exists while Participants are connected, plus a grace period.
_Avoid_: Channel, session

**Room Session**:
The control-plane lifecycle of a Room, from creation to close.
_Avoid_: Room (when you mean the lifecycle)

**Participant**:
A person or device joined to a Room.
_Avoid_: User, client, peer

**Roster**:
The current set of Participants in a Room and their roles.
_Avoid_: Member list, participant list

### Media

**Track**:
One media source published by a Participant, such as a camera or microphone.
_Avoid_: Stream, feed

**Stream**:
The transport-level unit SRS forwards for a Track. One Track is published as one Stream.
_Avoid_: Track (when you mean transport)

**Subscription**:
A Participant's request to receive another Participant's Track.
_Avoid_: Play, pull

**Latency Class**:
The product-level latency target: `ultra-low`, `low`, or `standard`. The implementation maps it to protocols.
_Avoid_: Latency mode, quality

### Recording and AI

**Recording**:
The captured media of a Meeting: one Recording Track per Participant plus the room event timeline.
_Avoid_: DVR, archive

**Recording Track**:
The per-Participant media file inside a Recording.
_Avoid_: Track (when you mean the recorded file)

**Artifact**:
An output produced from a Recording, such as a Transcript or Summary.
_Avoid_: Result, output

**Transcript**:
A timestamped, speaker-attributed text Artifact.
_Avoid_: Caption (when you mean the post-meeting transcript)

**Summary**:
A condensed text Artifact derived from a Transcript.
_Avoid_: Notes, minutes

**AI Job**:
A unit of work that turns a Recording into Artifacts.
_Avoid_: Task, job (when you mean the AI pipeline unit)

### Access

**Room Code**:
A short code a Participant uses to find a Room.
_Avoid_: Room ID (when you mean the human-facing code)

**Join Token**:
An HMAC-signed credential issued by the control plane for one Participant and Room, with an expiry.
_Avoid_: Session token, auth token
