import { describe, expect, it } from "vitest";
import type { PlaybackDescriptor } from "./api";
import { offlineMessage, playbackCandidates, shouldPoll } from "./playback";

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

describe("playbackCandidates", () => {
  it("tries HTTP-FLV before HLS", () => {
    expect(playbackCandidates(flv).map((candidate) => candidate.protocol)).toEqual(["flv", "hls"]);
  });

  it("flattens nested fallbacks in order", () => {
    const third: PlaybackDescriptor = {
      protocol: "whep",
      url: "http://127.0.0.1:8000/rtc/v1/whep/?app=live&stream=b1",
      expiresAt: 1,
      fallbacks: [],
    };
    const nested: PlaybackDescriptor = { ...hls, fallbacks: [third] };

    expect(playbackCandidates({ ...flv, fallbacks: [nested] }).map((c) => c.protocol)).toEqual([
      "flv",
      "hls",
      "whep",
    ]);
  });

  it("drops duplicate candidates", () => {
    expect(playbackCandidates({ ...flv, fallbacks: [hls, hls] })).toHaveLength(2);
  });
});

describe("offline transition", () => {
  it("reports offline after the Broadcast stops", () => {
    expect(offlineMessage("stopped")).toContain("Offline");
    expect(offlineMessage("failed")).toContain("Offline");
  });

  it("does not report offline for an active Broadcast", () => {
    expect(offlineMessage("live")).toBeNull();
    expect(offlineMessage("idle")).toBeNull();
    expect(offlineMessage("starting")).toBeNull();
  });
});

describe("shouldPoll", () => {
  it("polls while the Broadcast can still change state", () => {
    expect(shouldPoll("starting")).toBe(true);
    expect(shouldPoll("live")).toBe(true);
    expect(shouldPoll("stopping")).toBe(true);
  });

  it("stops polling in terminal states", () => {
    expect(shouldPoll("idle")).toBe(false);
    expect(shouldPoll("stopped")).toBe(false);
    expect(shouldPoll("failed")).toBe(false);
  });
});
