import { type FormEvent, useEffect, useState } from "react";
import {
  createMeeting,
  getRoom,
  type JoinOptions,
  joinRoom,
  kickParticipant,
  leaveRoom,
  type Role,
  type RoomCloseReason,
  type RosterEntry,
  roomSocketUrl,
  type WsEvent,
} from "./api";
import { errorMessage } from "./errors";
import { applyRoomEvent, parseRoomEvent } from "./room";
import { type RoomMembership, useRoomMedia } from "./useRoomMedia";

type ConnectionState = "idle" | "connecting" | "open" | "closed";

/** The control plane marks a Participant offline after 15 seconds without a heartbeat. */
const heartbeatIntervalMs = 5000;
/** Close reason the Room uses when the host removes a Participant. */
const kickedCloseReason: RoomCloseReason = "kicked";

type Membership = RoomMembership & {
  role: Role;
  displayName: string;
};

/** Enough identity to rejoin after an explicit leave. */
type RejoinState = Pick<Membership, "roomCode" | "displayName" | "participantId" | "joinToken">;

export function MeetingPanel() {
  const [hostName, setHostName] = useState("");
  const [roomCodeInput, setRoomCodeInput] = useState("");
  const [guestName, setGuestName] = useState("");
  const [membership, setMembership] = useState<Membership | null>(null);
  const [rejoin, setRejoin] = useState<RejoinState | null>(null);
  const [roster, setRoster] = useState<RosterEntry[]>([]);
  const [connection, setConnection] = useState<ConnectionState>("idle");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const media = useRoomMedia(membership);

  useEffect(() => {
    if (!membership) {
      return;
    }

    let cancelled = false;
    setConnection("connecting");

    const socket = new WebSocket(roomSocketUrl(membership.roomCode, membership.joinToken));
    const sendHeartbeat = () => {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "heartbeat" }));
      }
    };

    socket.onopen = () => {
      if (cancelled) {
        return;
      }
      setConnection("open");
      // The server replays the current Roster to a new subscriber, so events
      // between the join response and this socket are not lost.
      sendHeartbeat();
    };
    socket.onmessage = (event) => {
      if (cancelled) {
        return;
      }
      const update: WsEvent | null = parseRoomEvent(String(event.data));
      if (update) {
        setRoster((current) => applyRoomEvent(current, update));
        media.handleEvent(update);
      }
    };
    socket.onclose = (event) => {
      if (cancelled) {
        return;
      }
      if (event.reason === kickedCloseReason) {
        // The host removed this Participant. Return to the join form and do
        // not offer Rejoin: the Room refuses the kicked participantId.
        setConnection("idle");
        setError("You were removed from the Room.");
        setRoster([]);
        setMembership(null);
        return;
      }
      setConnection("closed");
    };
    socket.onerror = () => {
      if (!cancelled) {
        setConnection("closed");
      }
    };

    const heartbeat = setInterval(sendHeartbeat, heartbeatIntervalMs);
    return () => {
      cancelled = true;
      clearInterval(heartbeat);
      socket.close();
    };
  }, [membership, media.handleEvent]);

  function run(action: () => Promise<void>) {
    setBusy(true);
    setError(null);
    action()
      .catch((caught: unknown) => setError(errorMessage(caught)))
      .finally(() => setBusy(false));
  }

  async function enterRoom(roomCode: string, displayName: string, options: JoinOptions = {}) {
    const grant = await joinRoom(roomCode, displayName, options);
    const status = await getRoom(roomCode, grant.joinToken);
    const me = status.participants.find(
      (participant) => participant.participantId === grant.participantId,
    );

    setRoster(status.participants);
    setRejoin(null);
    setMembership({
      roomCode,
      participantId: grant.participantId,
      joinToken: grant.joinToken,
      publishUrl: grant.publishUrl,
      subscriptions: grant.subscriptions,
      role: me?.role ?? "guest",
      displayName,
    });
  }

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = hostName.trim();
    run(async () => {
      const meeting = await createMeeting(name);
      setRoomCodeInput(meeting.roomCode);
      await enterRoom(meeting.roomCode, name, { bearer: meeting.joinToken });
    });
  }

  function handleJoin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    run(() => enterRoom(roomCodeInput.trim().toUpperCase(), guestName.trim()));
  }

  function handleRejoin() {
    if (!rejoin) {
      return;
    }
    run(() =>
      enterRoom(rejoin.roomCode, rejoin.displayName, {
        bearer: rejoin.joinToken,
        participantId: rejoin.participantId,
      }),
    );
  }

  function handleLeave() {
    if (!membership) {
      return;
    }
    const leaving = membership;
    run(async () => {
      await leaveRoom(leaving.roomCode, leaving.participantId, leaving.joinToken);
      // Keep the identity so Rejoin reuses the same participantId.
      setRejoin({
        roomCode: leaving.roomCode,
        displayName: leaving.displayName,
        participantId: leaving.participantId,
        joinToken: leaving.joinToken,
      });
      setMembership(null);
      setRoster([]);
      setConnection("idle");
    });
  }

  return (
    <section>
      <h2>Meeting</h2>

      {!membership ? (
        <>
          <form onSubmit={handleCreate}>
            <label>
              Host name
              <input value={hostName} onChange={(event) => setHostName(event.target.value)} />
            </label>
            <button type="submit" disabled={busy || hostName.trim() === ""}>
              Create Meeting
            </button>
          </form>

          <form onSubmit={handleJoin}>
            <label>
              Room code
              <input
                value={roomCodeInput}
                maxLength={6}
                onChange={(event) => setRoomCodeInput(event.target.value)}
              />
            </label>
            <label>
              Your name
              <input value={guestName} onChange={(event) => setGuestName(event.target.value)} />
            </label>
            <button
              type="submit"
              disabled={busy || roomCodeInput.trim() === "" || guestName.trim() === ""}
            >
              Join Room
            </button>
          </form>

          {rejoin && (
            <p>
              You left {rejoin.roomCode}.{" "}
              <button type="button" data-testid="rejoin" onClick={handleRejoin} disabled={busy}>
                Rejoin as {rejoin.displayName}
              </button>
            </p>
          )}
        </>
      ) : (
        <div>
          <p>
            Room code: <strong data-testid="room-code">{membership.roomCode}</strong>
          </p>
          <p>
            Connection: <strong data-testid="room-connection">{connection}</strong>
          </p>
          <p>
            You are: <strong>{membership.role === "host" ? "host" : "guest"}</strong>{" "}
            <span data-testid="participant-id">{membership.participantId}</span>
          </p>
          <ul data-testid="roster">
            {roster.map((participant) => {
              // The server does not echo a Participant's own Track and mute
              // events back to them, so the self row reads local media state.
              const isSelf = participant.participantId === membership.participantId;
              const participantMuted = isSelf ? media.muted : participant.muted;
              // An empty Track set means the Participant has not reported yet;
              // only call the camera off once a publish is visible.
              const cameraOff = isSelf
                ? !media.cameraOn
                : participant.tracks.length > 0 &&
                  !participant.tracks.some((track) => track.kind === "video");
              return (
                <li
                  key={participant.participantId}
                  data-testid="roster-entry"
                  data-participant-id={participant.participantId}
                  data-muted={participantMuted}
                  data-online={participant.online}
                >
                  {participant.displayName} ({participant.role})
                  {!participant.online && <span data-testid="roster-offline"> — offline</span>}
                  {participantMuted && <span data-testid="roster-muted"> — muted</span>}
                  {cameraOff && <span data-testid="roster-camera-off"> — camera off</span>}
                  {participant.tracks.length > 0 && (
                    <span data-testid="roster-tracks">
                      {" "}
                      — {participant.tracks.map((track) => track.kind).join(" + ")}
                    </span>
                  )}
                  {membership.role === "host" && !isSelf && (
                    <button
                      type="button"
                      data-testid="roster-kick"
                      onClick={() =>
                        run(() =>
                          kickParticipant(
                            membership.roomCode,
                            participant.participantId,
                            membership.joinToken,
                          ),
                        )
                      }
                      disabled={busy}
                    >
                      Kick
                    </button>
                  )}
                </li>
              );
            })}
          </ul>

          <div>
            <p>
              Media: <strong data-testid="publish-state">{media.state}</strong>
            </p>
            {media.error && <p role="alert">{media.error}</p>}
            <div>
              <button
                type="button"
                data-testid="mute-toggle"
                onClick={() => run(media.toggleMute)}
                disabled={busy || media.state !== "live"}
              >
                {media.muted ? "Unmute" : "Mute"}
              </button>
              <button
                type="button"
                data-testid="camera-toggle"
                onClick={() => run(media.toggleCamera)}
                disabled={busy || media.state !== "live"}
              >
                {media.cameraOn ? "Camera Off" : "Camera On"}
              </button>
            </div>
            <video
              data-testid="local-video"
              autoPlay
              muted
              playsInline
              ref={(element) => {
                if (element && element.srcObject !== media.localStream) {
                  element.srcObject = media.localStream;
                }
              }}
            />
            <div data-testid="remote-videos">
              {[...media.remoteStreams].map(([participantId, stream]) => (
                // biome-ignore lint/a11y/useMediaCaption: live meeting media has no caption track
                <video
                  key={participantId}
                  data-testid={`remote-video-${participantId}`}
                  autoPlay
                  playsInline
                  ref={(element) => {
                    if (!element) {
                      return;
                    }
                    if (element.srcObject !== stream) {
                      element.srcObject = stream;
                    }
                    // Audio autoplay can be refused; a rejected play() leaves
                    // the element paused, so retry it as a best-effort.
                    void element.play().catch(() => {});
                  }}
                />
              ))}
            </div>
          </div>

          <button type="button" onClick={handleLeave} disabled={busy}>
            Leave
          </button>
        </div>
      )}

      {error && <p role="alert">{error}</p>}
    </section>
  );
}
