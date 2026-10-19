import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  type BroadcastSession,
  createBroadcast,
  getBroadcast,
  startBroadcast,
  stopBroadcast,
} from "./api";

const session: BroadcastSession = {
  broadcastId: "b1",
  status: "idle",
  latencyClass: "low",
  publish: { rtmpUrl: "rtmp://127.0.0.1:1935/live", streamKey: "b1" },
  playback: {
    protocol: "flv",
    url: "http://127.0.0.1:8080/live/b1.flv",
    expiresAt: 1,
    fallbacks: [],
  },
  viewerCount: 0,
};

function mockFetch(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockImplementation(() =>
    Promise.resolve(
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("broadcast api", () => {
  it("creates a Broadcast with its source and latency class", async () => {
    const fetchMock = mockFetch(session, 201);

    const result = await createBroadcast(
      { type: "file", path: "/tmp/fixture.mp4", loop: true },
      "low",
    );

    expect(result.broadcastId).toBe("b1");
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/broadcasts",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          source: { type: "file", path: "/tmp/fixture.mp4", loop: true },
          latencyClass: "low",
        }),
      }),
    );
  });

  it("uses the Broadcast-specific paths for status, start, and stop", async () => {
    const fetchMock = mockFetch(session);

    await getBroadcast("b1");
    await startBroadcast("b1");
    await stopBroadcast("b1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "/api/v1/broadcasts/b1",
      "/api/v1/broadcasts/b1/start",
      "/api/v1/broadcasts/b1/stop",
    ]);
  });

  it("surfaces the control-plane error message", async () => {
    mockFetch({ code: "not_found", message: "broadcast not found" }, 404);

    const error = await getBroadcast("missing").catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ message: "broadcast not found", status: 404 });
  });
});
