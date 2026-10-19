import { type FormEvent, useEffect, useRef, useState } from "react";
import {
  type BroadcastSession,
  createBroadcast,
  getBroadcast,
  startBroadcast,
  stopBroadcast,
} from "./api";
import { attachPlayer, type PlayerHandle } from "./player";

type SourceType = "file" | "camera";

export function BroadcastPanel() {
  const [sourceType, setSourceType] = useState<SourceType>("file");
  const [sourcePath, setSourcePath] = useState("");
  const [loop, setLoop] = useState(true);
  const [session, setSession] = useState<BroadcastSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const videoRef = useRef<HTMLVideoElement>(null);

  const playback = session?.status === "live" ? session.playback : null;

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !playback) {
      return;
    }

    let cancelled = false;
    let handle: PlayerHandle | null = null;

    attachPlayer(video, playback, setError)
      .then((created) => {
        if (cancelled) {
          created.destroy();
        } else {
          handle = created;
        }
      })
      .catch((caught: unknown) => {
        setError(caught instanceof Error ? caught.message : String(caught));
      });

    return () => {
      cancelled = true;
      handle?.destroy();
    };
  }, [playback]);

  function run(action: () => Promise<BroadcastSession>) {
    setBusy(true);
    setError(null);
    action()
      .then(setSession)
      .catch((caught: unknown) => {
        setError(caught instanceof Error ? caught.message : String(caught));
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
    run(() => createBroadcast(source, "low"));
  }

  return (
    <section>
      <h2>Broadcast</h2>
      <p>Latency: low</p>

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
        <button type="submit" disabled={busy || (sourceType === "file" && sourcePath === "")}>
          Create Broadcast
        </button>
      </form>

      {error && <p role="alert">{error}</p>}

      {session && (
        <div>
          <p>
            Status: <strong>{session.status}</strong>
          </p>
          <p>
            Publish: <code>{session.publish.rtmpUrl}</code> stream key{" "}
            <code>{session.publish.streamKey}</code>
          </p>
          <p>
            Playback: <code>{session.playback.url}</code>
          </p>
          <button
            type="button"
            disabled={busy || session.status === "live"}
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
