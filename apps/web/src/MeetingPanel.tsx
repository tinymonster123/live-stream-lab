import { type FormEvent, useEffect, useState } from "react";
import {
  createMeeting,
  getRoom,
  joinRoom,
  leaveRoom,
  type Role,
  type RosterEntry,
  roomSocketUrl,
  type WsEvent,
} from "./api";
import { errorMessage } from "./errors";
import { applyRoomEvent, parseRoomEvent } from "./room";
import { type RoomMembership, useRoomMedia } from "./useRoomMedia";

type ConnectionState = "idle" | "connecting" | "open" | "closed";

type Membership = RoomMembership & {
  role: Role;
};

export function MeetingPanel() {
  const [hostName, setHostName] = useState("");
  const [roomCodeInput, setRoomCodeInput] = useState("");
  const [guestName, setGuestName] = useState("");
  const [membership, setMembership] = useState<Membership | null>(null);
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
    socket.onopen = () => {
      if (cancelled) {
        return;
      }
      setConnection("open");
      // The server replays the current Roster to a new subscriber, so events
      // between the join response and this socket are not lost.
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
    socket.onclose = () => {
      if (!cancelled) {
        setConnection("closed");
      }
    };
    socket.onerror = () => {
      if (!cancelled) {
        setConnection("closed");
      }
    };

    return () => {
      cancelled = true;
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

  async function enterRoom(roomCode: string, displayName: string, hostToken?: string) {
    const grant = await joinRoom(roomCode, displayName, hostToken);
    const status = await getRoom(roomCode, grant.joinToken);
    const me = status.participants.find(
      (participant) => participant.participantId === grant.participantId,
    );

    setRoster(status.participants);
    setMembership({
      roomCode,
      participantId: grant.participantId,
      joinToken: grant.joinToken,
      publishUrl: grant.publishUrl,
      subscriptions: grant.subscriptions,
      role: me?.role ?? "guest",
    });
  }

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = hostName.trim();
    run(async () => {
      const meeting = await createMeeting(name);
      setRoomCodeInput(meeting.roomCode);
      await enterRoom(meeting.roomCode, name, meeting.joinToken);
    });
  }

  function handleJoin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    run(() => enterRoom(roomCodeInput.trim().toUpperCase(), guestName.trim()));
  }

  function handleLeave() {
    if (!membership) {
      return;
    }
    run(async () => {
      await leaveRoom(membership.roomCode, membership.participantId, membership.joinToken);
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
            You are: <strong>{membership.role === "host" ? "host" : "guest"}</strong>
          </p>
          <ul data-testid="roster">
            {roster.map((participant) => (
              <li key={participant.participantId}>
                {participant.displayName} ({participant.role})
                {participant.tracks.length > 0 && (
                  <span data-testid="roster-tracks">
                    {" "}
                    — {participant.tracks.map((track) => track.kind).join(" + ")}
                  </span>
                )}
              </li>
            ))}
          </ul>

          <div>
            <p>
              Media: <strong data-testid="publish-state">{media.state}</strong>
            </p>
            {media.error && <p role="alert">{media.error}</p>}
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
