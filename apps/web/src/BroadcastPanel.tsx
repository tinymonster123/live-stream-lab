import { type FormEvent, useEffect, useRef, useState } from "react";
import {
  type BroadcastSession,
  createBroadcast,
  getBroadcast,
  type LatencyClass,
  type PlaybackDescriptor,
  startBroadcast,
  stopBroadcast,
} from "./api";
import { errorMessage } from "./errors";
import { descriptorKey, offlineMessage, shouldPoll } from "./playback";
import { attachPlayer, type PlayerHandle } from "./player";

type SourceType = "file" | "camera";

const POLL_INTERVAL_MS = 3000;

/**
 * The status block is a pure render of one session so the offline transition
 * can be tested without driving the player or the network.
 */
export function BroadcastStatus({
  session,
  protocol,
}: {
  session: BroadcastSession;
  protocol?: PlaybackDescriptor["protocol"] | null;
}) {
  const offline = offlineMessage(session.status);

  return (
    <div>
      <p>
        Status: <strong>{session.status}</strong>
      </p>
      <p>
        Viewers: <strong data-testid="viewer-count">{session.viewerCount}</strong>
      </p>
      {protocol && (
        <p>
          Protocol: <code>{protocol}</code>
        </p>
      )}
      {offline && (
        <p role="status">
          <strong>{offline}</strong>
        </p>
      )}
    </div>
  );
}

export function BroadcastPanel() {
  const [sourceType, setSourceType] = useState<SourceType>("file");
  const [sourcePath, setSourcePath] = useState("");
  const [loop, setLoop] = useState(true);
  const [latencyClass, setLatencyClass] = useState<LatencyClass>("low");
  const [session, setSession] = useState<BroadcastSession | null>(null);
  const [protocol, setProtocol] = useState<PlaybackDescriptor["protocol"] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);

  const livePlayback = session?.status === "live" ? session.playback : null;
  // Polls return a new object with the same content; key the player effect on
  // the descriptor's identity so polling does not tear down playback.
  const playbackKey = livePlayback ? descriptorKey(livePlayback) : null;

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !livePlayback) {
      return;
    }

    let cancelled = false;
    let handle: PlayerHandle | null = null;
    setProtocol(null);

    attachPlayer(video, livePlayback, {
      onError: setError,
      onProtocolChange: setProtocol,
    })
      .then((created) => {
        if (cancelled) {
          created.destroy();
        } else {
          handle = created;
        }
      })
      .catch((caught: unknown) => {
        if (!cancelled) {
          setError(errorMessage(caught));
        }
      });

    return () => {
      cancelled = true;
      handle?.destroy();
    };
    // Only the descriptor content matters; a poll with the same content must
    // not tear down and re-attach the player.
  }, [playbackKey]);

  useEffect(() => {
    if (!session || !shouldPoll(session.status)) {
      return;
    }

    const broadcastId = session.broadcastId;
    const timer = setInterval(() => {
      getBroadcast(broadcastId)
        .then((next) => {
          setSession(next);
          if (next.status !== "live") {
            setProtocol(null);
          }
        })
        .catch((caught: unknown) => {
          setError(errorMessage(caught));
        });
    }, POLL_INTERVAL_MS);

    return () => clearInterval(timer);
  }, [session?.broadcastId, session?.status]);

  function run(action: () => Promise<BroadcastSession>) {
    setBusy(true);
    setError(null);
    action()
      .then((next) => {
        setSession(next);
        if (next.status !== "live") {
          setProtocol(null);
        }
      })
      .catch((caught: unknown) => {
        setError(errorMessage(caught));
        if (session) {
          void getBroadcast(session.broadcastId)
            .then(setSession)
            .catch(() => {});
        }
      })
      .finally(() => setBusy(false));
  }

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const source =
      sourceType === "file"
        ? { type: "file" as const, path: sourcePath, loop }
        : { type: "camera" as const, path: sourcePath || undefined };
    run(() => createBroadcast(source, latencyClass));
  }

  return (
    <section>
      <h2>Broadcast</h2>

      <form onSubmit={handleCreate}>
        <label>
          Source
          <select
            value={sourceType}
            onChange={(event) => setSourceType(event.target.value as SourceType)}
          >
            <option value="file">File</option>
            <option value="camera">Camera</option>
          </select>
        </label>
        <label>
          {sourceType === "file" ? "File path" : "avfoundation device"}
          <input
            value={sourcePath}
            placeholder={sourceType === "file" ? "/tmp/fixture.mp4" : "0:0"}
            onChange={(event) => setSourcePath(event.target.value)}
          />
        </label>
        {sourceType === "file" && (
          <label>
            <input
              type="checkbox"
              checked={loop}
              onChange={(event) => setLoop(event.target.checked)}
            />
            Loop file
          </label>
        )}
        <label>
          Latency
          <select
            value={latencyClass}
            onChange={(event) => setLatencyClass(event.target.value as LatencyClass)}
          >
            <option value="low">low (HTTP-FLV, HLS fallback)</option>
            <option value="standard">standard (HLS)</option>
          </select>
        </label>
        <button type="submit" disabled={busy || (sourceType === "file" && sourcePath === "")}>
          Create Broadcast
        </button>
      </form>

      {error && <p role="alert">{error}</p>}

      {session && (
        <div>
          <BroadcastStatus
            session={session}
            protocol={session.status === "live" ? protocol : null}
          />
          <p>
            Publish: <code>{session.publish.rtmpUrl}</code> stream key{" "}
            <code>{session.publish.streamKey}</code>
          </p>
          <p>
            Playback: <code>{session.playback.url}</code>
            {session.playback.fallbacks.map((fallback) => (
              <span key={descriptorKey(fallback)}>
                {" "}
                → <code>{fallback.url}</code>
              </span>
            ))}
          </p>
          <button
            type="button"
            disabled={busy || session.status === "live" || session.status === "starting"}
            onClick={() => run(() => startBroadcast(session.broadcastId))}
          >
            Start
          </button>
          <button
            type="button"
            disabled={busy || session.status === "stopped"}
            onClick={() => run(() => stopBroadcast(session.broadcastId))}
          >
            Stop
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => run(() => getBroadcast(session.broadcastId))}
          >
            Refresh
          </button>
        </div>
      )}

      <video ref={videoRef} controls muted playsInline width={640} />
    </section>
  );
}
