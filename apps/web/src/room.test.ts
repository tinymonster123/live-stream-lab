import { describe, expect, it } from "vitest";
import type { RosterEntry, WsEvent } from "./api";
import { applyRoomEvent, parseRoomEvent } from "./room";

const host: RosterEntry = {
  participantId: "p-host",
  displayName: "Alice",
  role: "host",
  tracks: [],
};

const guest: RosterEntry = {
  participantId: "p-guest",
  displayName: "Bob",
  role: "guest",
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

  it("ignores events that do not affect the Roster", () => {
    const event: WsEvent = {
      type: "mute.changed",
      participantId: guest.participantId,
      muted: true,
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
