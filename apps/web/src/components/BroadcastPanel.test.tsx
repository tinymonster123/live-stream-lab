import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { BroadcastSession } from "../lib/api";
import { BroadcastStatus } from "./BroadcastPanel";

function session(overrides: Partial<BroadcastSession>): BroadcastSession {
  return {
    broadcastId: "b1",
    status: "idle",
    latencyClass: "low",
    publish: { rtmpUrl: "rtmp://127.0.0.1:1935/live", streamKey: "b1" },
    playback: {
      protocol: "flv",
      url: "http://127.0.0.1:8080/live/b1.flv",
      expiresAt: 1,
      fallbacks: [
        { protocol: "hls", url: "http://127.0.0.1:8080/live/b1.m3u8", expiresAt: 1, fallbacks: [] },
      ],
    },
    viewerCount: 0,
    ...overrides,
  };
}

describe("BroadcastStatus", () => {
  it("shows the live status and viewer count", () => {
    const html = renderToString(
      <BroadcastStatus session={session({ status: "live", viewerCount: 3 })} />,
    );

    expect(html).toContain("live");
    expect(html).toContain("3");
    expect(html).not.toContain("Offline");
  });

  it("shows an offline state when the Broadcast is stopped", () => {
    const html = renderToString(<BroadcastStatus session={session({ status: "stopped" })} />);

    expect(html).toContain("Offline");
    expect(html).toContain("stopped");
  });

  it("shows an offline state when the Broadcast fails", () => {
    const html = renderToString(<BroadcastStatus session={session({ status: "failed" })} />);

    expect(html).toContain("Offline");
    expect(html).toContain("failed");
  });
});
