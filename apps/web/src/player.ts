import type { PlaybackDescriptor } from "./api";
import { errorMessage } from "./errors";
import { playbackCandidates } from "./playback";

export interface PlayerHandle {
  destroy(): void;
}

export interface PlayerCallbacks {
  /** A playable protocol dropped after it had started, or the last attempt failed. */
  onError?: (message: string) => void;
  /** The protocol the player actually attached to. */
  onProtocolChange?: (protocol: PlaybackDescriptor["protocol"]) => void;
}

// Both protocols are reported ready only after media actually arrives; a
// reachable-but-broken endpoint must fall through to the next candidate.
const FLV_START_TIMEOUT_MS = 5000;
const HLS_START_TIMEOUT_MS = 10000;

export async function attachPlayer(
  video: HTMLVideoElement,
  playback: PlaybackDescriptor,
  callbacks: PlayerCallbacks = {},
): Promise<PlayerHandle> {
  const problems: string[] = [];

  for (const candidate of playbackCandidates(playback)) {
    try {
      const handle = await attachCandidate(video, candidate, callbacks);
      callbacks.onProtocolChange?.(candidate.protocol);
      return handle;
    } catch (caught) {
      problems.push(`${candidate.protocol}: ${errorMessage(caught)}`);
    }
  }

  const detail = problems.length > 0 ? ` (${problems.join("; ")})` : "";
  throw new Error(`No playable protocol${detail}`);
}

async function attachCandidate(
  video: HTMLVideoElement,
  candidate: PlaybackDescriptor,
  callbacks: PlayerCallbacks,
): Promise<PlayerHandle> {
  switch (candidate.protocol) {
    case "flv":
      return attachFlv(video, candidate.url, callbacks);
    case "hls":
      return attachNativeHls(video, candidate.url, callbacks);
    default:
      throw new Error(`unsupported protocol ${candidate.protocol}`);
  }
}

async function attachFlv(
  video: HTMLVideoElement,
  url: string,
  callbacks: PlayerCallbacks,
): Promise<PlayerHandle> {
  const { default: mpegts } = await import("mpegts.js");

  if (!mpegts.isSupported()) {
    throw new Error("HTTP-FLV is not supported in this browser");
  }

  const player = mpegts.createPlayer(
    { type: "flv", url, isLive: true },
    { enableStashBuffer: false },
  );

  let started = false;
  let destroyed = false;

  const handle: PlayerHandle = {
    destroy() {
      if (destroyed) {
        return;
      }
      destroyed = true;
      player.pause();
      player.unload();
      player.detachMediaElement();
      player.destroy();
    },
  };

  return new Promise<PlayerHandle>((resolve, reject) => {
    const timer = setTimeout(() => {
      if (started) {
        return;
      }
      handle.destroy();
      reject(new Error("HTTP-FLV did not produce media"));
    }, FLV_START_TIMEOUT_MS);

    player.on(mpegts.Events.MEDIA_INFO, () => {
      started = true;
      clearTimeout(timer);
      resolve(handle);
    });

    player.on(mpegts.Events.ERROR, (type: string, detail: string) => {
      const message = `Playback error: ${type}${detail ? ` (${detail})` : ""}`;
      clearTimeout(timer);

      if (!started) {
        handle.destroy();
        reject(new Error(message));
        return;
      }
      callbacks.onError?.(message);
    });

    player.attachMediaElement(video);
    player.load();
    const playbackPromise = player.play();
    if (playbackPromise) {
      void playbackPromise.catch(() => {});
    }
  });
}

function attachNativeHls(
  video: HTMLVideoElement,
  url: string,
  callbacks: PlayerCallbacks,
): Promise<PlayerHandle> {
  // Safari (and iOS) play HLS natively. Other browsers need an MSE-based HLS
  // library, which is out of scope until a second browser must be supported.
  if (video.canPlayType("application/vnd.apple.mpegurl") === "") {
    return Promise.reject(new Error("HLS is not supported in this browser"));
  }

  const describeError = () => `HLS playback error: ${video.error?.message ?? "unknown"}`;

  return new Promise<PlayerHandle>((resolve, reject) => {
    let started = false;
    let destroyed = false;
    let timer: ReturnType<typeof setTimeout>;

    const handle: PlayerHandle = {
      destroy() {
        if (destroyed) {
          return;
        }
        destroyed = true;
        clearTimeout(timer);
        video.removeEventListener("error", onError);
        video.removeEventListener("loadedmetadata", onLoaded);
        video.removeAttribute("src");
        video.load();
      },
    };

    function onLoaded() {
      if (started) {
        return;
      }
      started = true;
      clearTimeout(timer);
      resolve(handle);
    }

    function onError() {
      if (started) {
        callbacks.onError?.(describeError());
        return;
      }
      handle.destroy();
      reject(new Error(describeError()));
    }

    timer = setTimeout(() => {
      if (started) {
        return;
      }
      handle.destroy();
      reject(new Error("HLS did not load"));
    }, HLS_START_TIMEOUT_MS);

    video.addEventListener("loadedmetadata", onLoaded);
    video.addEventListener("error", onError);
    video.src = url;
    const playbackPromise = video.play();
    if (playbackPromise) {
      void playbackPromise.catch(() => {});
    }
  });
}
