# Record per Participant with FFmpeg and process locally with MLX Whisper

Status: accepted

For every Meeting, the Go control plane starts one FFmpeg recorder per Participant, pulling the RTC-to-RTMP Stream from SRS. Files are per-Participant Recording Tracks plus an event timeline. Post-meeting AI Jobs go through Redis Streams to a native Python worker; the first Artifacts are a Transcript (mlx-whisper) and an extractive Summary. Real-time captions and LLM summaries come later.

## Considered Options

- **SRS DVR**: static vhost configuration, awkward to start and stop per Meeting.
- **Client MediaRecorder**: depends on the client staying open and connected; unreliable for Meetings.
- **All-Compose AI worker**: Docker Desktop runs Linux; Metal and MLX are unavailable.

## Consequences

- N Participants mean N FFmpeg processes; acceptable for 1:1 and 2–6 person Rooms.
- The Python worker must run natively on the macOS host.
- Redis Streams carries `AI Job` messages; `meetingId + recordingId` is the idempotency key.
- Recordings live on local disk; SQLite stores Meeting, Participant, Recording, and Artifact metadata.
