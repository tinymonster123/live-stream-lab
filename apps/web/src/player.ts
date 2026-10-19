import type { PlaybackDescriptor } from "./api";

export interface PlayerHandle {
  destroy(): void;
}

export async function attachPlayer(
  video: HTMLVideoElement,
  playback: PlaybackDescriptor,
  onError: (message: string) => void,
): Promise<PlayerHandle> {
  const { default: mpegts } = await import("mpegts.js");

  if (!mpegts.isSupported()) {
    throw new Error("HTTP-FLV playback is not supported in this browser");
  }
  if (playback.protocol !== "flv") {
    throw new Error(`Unsupported playback protocol: ${playback.protocol}`);
  }

  const player = mpegts.createPlayer(
    { type: "flv", url: playback.url, isLive: true },
    { enableStashBuffer: false },
  );

  player.on(mpegts.Events.ERROR, (type: string, detail: string) => {
    onError(`Playback error: ${type}${detail ? ` (${detail})` : ""}`);
  });

  player.attachMediaElement(video);
  player.load();
  void player.play();

  return {
    destroy() {
      player.pause();
      player.unload();
      player.detachMediaElement();
      player.destroy();
    },
  };
}
