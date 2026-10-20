import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  type BroadcastSession,
  createBroadcast,
  createMeeting,
  getBroadcast,
  getRoom,
  type JoinGrant,
  joinRoom,
  leaveRoom,
  type MeetingCreated,
  type RoomStatus,
  setMuted,
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

const meeting: MeetingCreated = {
  meetingId: "m1",
  roomCode: "ABC234",
  joinToken: "host-token",
  expiresAt: "2026-10-20T12:00:00Z",
};

const grant: JoinGrant = {
  participantId: "p1",
  joinToken: "participant-token",
  publishUrl: "http://127.0.0.1:8080/rtc/v1/whip/?app=live&stream=p1",
  subscriptions: [],
};

const room: RoomStatus = {
  roomCode: "ABC234",
  state: "open",
  participants: [
    {
      participantId: "p1",
      displayName: "Alice",
      role: "host",
      muted: false,
      tracks: [],
    },
  ],
};

describe("meeting api", () => {
  it("creates a meeting with the host name", async () => {
    const fetchMock = mockFetch(meeting, 201);

    await createMeeting("Alice");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/meetings",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ hostDisplayName: "Alice" }),
      }),
    );
  });

  it("joins with the host token when one is provided", async () => {
    const fetchMock = mockFetch(grant);

    await joinRoom("ABC234", "Alice", { bearer: "host-token" });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234/join",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ displayName: "Alice" }),
        headers: expect.objectContaining({ Authorization: "Bearer host-token" }),
      }),
    );
  });

  it("joins anonymously without a host token", async () => {
    const fetchMock = mockFetch(grant);

    await joinRoom("ABC234", "Bob");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234/join",
      expect.objectContaining({
        headers: { "Content-Type": "application/json" },
      }),
    );
  });

  it("reads the roster with the participant token", async () => {
    const fetchMock = mockFetch(room);

    const status = await getRoom("ABC234", "participant-token");

    expect(status.participants).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234",
      expect.objectContaining({
        headers: expect.objectContaining({ Authorization: "Bearer participant-token" }),
      }),
    );
  });

  it("rejoins with the existing participantId and token", async () => {
    const fetchMock = mockFetch(grant);

    await joinRoom("ABC234", "Bob", { bearer: "participant-token", participantId: "p1" });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234/join",
      expect.objectContaining({
        body: JSON.stringify({ displayName: "Bob", participantId: "p1" }),
        headers: expect.objectContaining({ Authorization: "Bearer participant-token" }),
      }),
    );
  });

  it("updates mute state with the participant token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(setMuted("ABC234", "p1", true, "participant-token")).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234/mute",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ participantId: "p1", muted: true }),
        headers: expect.objectContaining({ Authorization: "Bearer participant-token" }),
      }),
    );
  });

  it("leaves with the participant token and accepts 204", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(leaveRoom("ABC234", "p1", "participant-token")).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/rooms/ABC234/leave",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ participantId: "p1" }),
        headers: expect.objectContaining({ Authorization: "Bearer participant-token" }),
      }),
    );
  });
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
