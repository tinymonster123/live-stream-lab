import { describe, expect, it } from "vitest";
import type { RosterEntry, WsEvent } from "./api";
import { applyRoomEvent, parseRoomEvent } from "./room";

const host: RosterEntry = {
  participantId: "p-host",
  displayName: "Alice",
  role: "host",
  muted: false,
  tracks: [],
};

const guest: RosterEntry = {
  participantId: "p-guest",
  displayName: "Bob",
  role: "guest",
  muted: false,
  tracks: [],
};

describe("applyRoomEvent", () => {
  it("adds a participant when they join", () => {
    const event: WsEvent = {
      type: "participant.joined",
      participantId: guest.participantId,
      displayName: guest.displayName,
      role: "guest",
    };

    expect(applyRoomEvent([host], event)).toEqual([host, guest]);
  });

  it("updates a participant without duplicating them", () => {
    const event: WsEvent = {
      type: "participant.joined",
      participantId: guest.participantId,
      displayName: "Robert",
      role: "guest",
    };

    const next = applyRoomEvent([host, guest], event);
    expect(next).toHaveLength(2);
    expect(next[1]?.displayName).toBe("Robert");
  });

  it("removes a participant when they leave", () => {
    const event: WsEvent = { type: "participant.left", participantId: guest.participantId };

    expect(applyRoomEvent([host, guest], event)).toEqual([host]);
  });

  it("adds a published Track to its Participant", () => {
    const event: WsEvent = {
      type: "track.published",
      participantId: guest.participantId,
      trackId: "camera",
      kind: "video",
      whepUrl: "/rtc/v1/whep/?app=live&stream=p-guest",
    };

    const next = applyRoomEvent([host, guest], event);
    expect(next[1]?.tracks).toEqual([{ trackId: "camera", kind: "video", published: true }]);
  });

  it("replaces a Track without duplicating it", () => {
    const published: WsEvent = {
      type: "track.published",
      participantId: guest.participantId,
      trackId: "camera",
      kind: "video",
      whepUrl: "/rtc/v1/whep/?app=live&stream=p-guest",
    };
    const once = applyRoomEvent([host, guest], published);
    const twice = applyRoomEvent(once, published);

    expect(twice[1]?.tracks).toHaveLength(1);
  });

  it("removes an unpublished Track", () => {
    const withTrack: RosterEntry = {
      ...guest,
      tracks: [{ trackId: "camera", kind: "video", published: true }],
    };

    const next = applyRoomEvent([host, withTrack], {
      type: "track.unpublished",
      participantId: guest.participantId,
      trackId: "camera",
    });

    expect(next[1]?.tracks).toEqual([]);
  });

  it("marks a muted Participant", () => {
    const event: WsEvent = {
      type: "mute.changed",
      participantId: guest.participantId,
      muted: true,
    };

    const next = applyRoomEvent([host, guest], event);
    expect(next[1]?.muted).toBe(true);
    expect(next[0]?.muted).toBe(false);
  });

  it("keeps Track and mute state across a rejoin event", () => {
    const joined: RosterEntry = {
      ...guest,
      muted: true,
      tracks: [{ trackId: "microphone", kind: "audio", published: true }],
    };

    const next = applyRoomEvent([host, joined], {
      type: "participant.joined",
      participantId: guest.participantId,
      displayName: "Robert",
      role: "guest",
    });

    expect(next[1]).toMatchObject({
      displayName: "Robert",
      muted: true,
      tracks: [{ trackId: "microphone", kind: "audio", published: true }],
    });
  });

  it("ignores events that do not affect the Roster", () => {
    const event: WsEvent = {
      type: "recording.state_changed",
      recordingId: "r1",
      state: "recording",
    };

    expect(applyRoomEvent([host, guest], event)).toEqual([host, guest]);
  });
});

describe("parseRoomEvent", () => {
  it("parses a participant event", () => {
    expect(
      parseRoomEvent(
        JSON.stringify({
          type: "participant.joined",
          participantId: "p1",
          displayName: "Alice",
          role: "host",
        }),
      ),
    ).toMatchObject({ type: "participant.joined", participantId: "p1" });
  });

  it("returns null for malformed payloads", () => {
    expect(parseRoomEvent("not json")).toBeNull();
    expect(parseRoomEvent("null")).toBeNull();
    expect(parseRoomEvent(`{"displayName":"Alice"}`)).toBeNull();
  });
});
