import type { RosterEntry, WsEvent } from "./api";

/** Applies a server event to the Roster without mutating the current list. */
export function applyRoomEvent(roster: RosterEntry[], event: WsEvent): RosterEntry[] {
  switch (event.type) {
    case "participant.joined": {
      const entry: RosterEntry = {
        participantId: event.participantId,
        displayName: event.displayName,
        role: event.role,
        tracks: [],
      };
      const index = roster.findIndex((item) => item.participantId === event.participantId);
      if (index === -1) {
        return [...roster, entry];
      }

      const current = roster[index];
      if (!current) {
        return [...roster, entry];
      }
      const next = roster.slice();
      next[index] = { ...current, ...entry, tracks: current.tracks };
      return next;
    }

    case "participant.left":
      return roster.filter((item) => item.participantId !== event.participantId);

    default:
      // T5 only consumes Roster events; track/recording events arrive later.
      return roster;
  }
}

export function parseRoomEvent(raw: string): WsEvent | null {
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== "object" || value === null || !("type" in value)) {
      return null;
    }
    if (typeof value.type !== "string") {
      return null;
    }
    return value as WsEvent;
  } catch {
    return null;
  }
}
