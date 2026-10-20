import { describe, expect, it, vi } from "vitest";
import type { PlaybackDescriptor } from "./api";
import { attachPlayer } from "./player";

interface FakePlayer {
  destroyed: boolean;
  handlers: Map<string, (...args: unknown[]) => void>;
  on(event: string, handler: (...args: unknown[]) => void): void;
}

const { flvPlayers } = vi.hoisted(() => ({
  flvPlayers: [] as FakePlayer[],
}));

vi.mock("mpegts.js", () => {
  const createPlayer = () => {
    const handlers = new Map<string, (...args: unknown[]) => void>();
    const player: FakePlayer = {
      destroyed: false,
      handlers,
      on(event, handler) {
        handlers.set(event, handler);
      },
    };
    flvPlayers.push(player);

    return {
      on: player.on,
      attachMediaElement() {},
      // An unreachable FLV endpoint surfaces as an ERROR event, not a throw.
      load() {
        queueMicrotask(() => handlers.get("error")?.("NetworkError", "HTTP 404"));
      },
      play: () => Promise.resolve(),
      pause() {},
      unload() {},
      detachMediaElement() {},
      destroy() {
        player.destroyed = true;
      },
    };
  };

  return {
    default: {
      isSupported: () => true,
      createPlayer,
      Events: { ERROR: "error", MEDIA_INFO: "media_info" },
    },
  };
});

class FakeVideo {
  canPlayHls = true;
  error: { message: string } | null = null;
  private listeners = new Map<string, Set<() => void>>();
  private source = "";

  canPlayType(): string {
    return this.canPlayHls ? "maybe" : "";
  }

  addEventListener(type: string, listener: () => void) {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: () => void) {
    this.listeners.get(type)?.delete(listener);
  }

  removeAttribute(name: string) {
    if (name === "src") {
      this.source = "";
    }
  }

  load() {}

  play(): Promise<void> {
    return Promise.resolve();
  }

  get src(): string {
    return this.source;
  }

  set src(value: string) {
    this.source = value;
    // Native HLS reports readiness through the media element, not a promise.
    queueMicrotask(() => {
      for (const listener of this.listeners.get("loadedmetadata") ?? []) {
        listener();
      }
    });
  }
}

const hls: PlaybackDescriptor = {
  protocol: "hls",
  url: "http://127.0.0.1:8080/live/b1.m3u8",
  expiresAt: 1,
  fallbacks: [],
};

const flv: PlaybackDescriptor = {
  protocol: "flv",
  url: "http://127.0.0.1:8080/live/b1.flv",
  expiresAt: 1,
  fallbacks: [hls],
};

describe("attachPlayer", () => {
  it("falls back to HLS when HTTP-FLV cannot start", async () => {
    flvPlayers.length = 0;
    const video = new FakeVideo();
    const protocols: string[] = [];
    const errors: string[] = [];

    const handle = await attachPlayer(video as unknown as HTMLVideoElement, flv, {
      onProtocolChange: (protocol) => protocols.push(protocol),
      onError: (message) => errors.push(message),
    });

    expect(protocols).toEqual(["hls"]);
    expect(video.src).toBe(hls.url);
    expect(flvPlayers[0]?.destroyed).toBe(true);
    expect(errors).toEqual([]);

    handle.destroy();
    expect(video.src).toBe("");
  });

  it("reports every candidate when none can play", async () => {
    flvPlayers.length = 0;
    const video = new FakeVideo();
    video.canPlayHls = false;

    await expect(attachPlayer(video as unknown as HTMLVideoElement, flv, {})).rejects.toThrow(
      /No playable protocol.*flv.*hls/,
    );
  });
});
