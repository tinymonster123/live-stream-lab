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

    case "track.published":
      return roster.map((item) => {
        if (item.participantId !== event.participantId) {
          return item;
        }

        const track = {
          trackId: event.trackId,
          kind: event.kind,
          published: true,
        };
        const replaced = item.tracks.some((current) => current.trackId === event.trackId);
        const tracks = replaced
          ? item.tracks.map((current) =>
              current.trackId === event.trackId ? { ...current, ...track } : current,
            )
          : [...item.tracks, track];
        return { ...item, tracks };
      });

    case "track.unpublished":
      return roster.map((item) =>
        item.participantId === event.participantId
          ? {
              ...item,
              tracks: item.tracks.filter((track) => track.trackId !== event.trackId),
            }
          : item,
      );

    default:
      // Recording and AI events are not part of the Roster.
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
