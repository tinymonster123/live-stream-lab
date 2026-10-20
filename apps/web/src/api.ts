import type { components } from "../../../libs/contracts/generated/api";

export type BroadcastSession = components["schemas"]["BroadcastSession"];
export type BroadcastSource = components["schemas"]["BroadcastSource"];
export type JoinGrant = components["schemas"]["JoinGrant"];
export type LatencyClass = components["schemas"]["LatencyClass"];
export type MeetingCreated = components["schemas"]["MeetingCreated"];
export type PlaybackDescriptor = components["schemas"]["PlaybackDescriptor"];
export type Role = components["schemas"]["RosterEntry"]["role"];
export type RoomStatus = components["schemas"]["RoomStatus"];
export type RosterEntry = components["schemas"]["RosterEntry"];
export type RoomCloseReason = components["schemas"]["RoomCloseReason"];
export type RoomJoinRequest = components["schemas"]["RoomJoinRequest"];
export type RoomKickRequest = components["schemas"]["RoomKickRequest"];
export type RoomMuteUpdate = components["schemas"]["RoomMuteUpdate"];
export type RoomTrack = components["schemas"]["RoomTrack"];
export type SubscriptionTarget = components["schemas"]["SubscriptionTarget"];
export type TrackSummary = components["schemas"]["TrackSummary"];
export type WsEvent = components["schemas"]["WsEvent"];

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

const API_BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });

  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { message?: string };
      if (body.message) {
        message = body.message;
      }
    } catch {
      // Keep the status-based message when the body is not JSON.
    }
    throw new ApiError(message, response.status);
  }

  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export function createBroadcast(
  source: BroadcastSource,
  latencyClass: LatencyClass,
): Promise<BroadcastSession> {
  return request<BroadcastSession>("/broadcasts", {
    method: "POST",
    body: JSON.stringify({ source, latencyClass }),
  });
}

export function getBroadcast(broadcastId: string): Promise<BroadcastSession> {
  return request<BroadcastSession>(`/broadcasts/${broadcastId}`);
}

export function startBroadcast(broadcastId: string): Promise<BroadcastSession> {
  return request<BroadcastSession>(`/broadcasts/${broadcastId}/start`, { method: "POST" });
}

export function stopBroadcast(broadcastId: string): Promise<BroadcastSession> {
  return request<BroadcastSession>(`/broadcasts/${broadcastId}/stop`, { method: "POST" });
}

export function createMeeting(hostDisplayName: string): Promise<MeetingCreated> {
  return request<MeetingCreated>("/meetings", {
    method: "POST",
    body: JSON.stringify({ hostDisplayName }),
  });
}

export type JoinOptions = {
  /** The Meeting host token, or the Participant token when rejoining. */
  bearer?: string;
  /** Reuse an existing identity after leaving or a network drop. */
  participantId?: string;
};

export function joinRoom(
  roomCode: string,
  displayName: string,
  options: JoinOptions = {},
): Promise<JoinGrant> {
  const body: RoomJoinRequest = { displayName };
  if (options.participantId) {
    body.participantId = options.participantId;
  }
  return request<JoinGrant>(`/rooms/${encodeURIComponent(roomCode)}/join`, {
    method: "POST",
    body: JSON.stringify(body),
    headers: options.bearer ? { Authorization: `Bearer ${options.bearer}` } : undefined,
  });
}

export function getRoom(roomCode: string, joinToken: string): Promise<RoomStatus> {
  return request<RoomStatus>(`/rooms/${encodeURIComponent(roomCode)}`, {
    headers: { Authorization: `Bearer ${joinToken}` },
  });
}

export function leaveRoom(
  roomCode: string,
  participantId: string,
  joinToken: string,
): Promise<void> {
  return request<void>(`/rooms/${encodeURIComponent(roomCode)}/leave`, {
    method: "POST",
    body: JSON.stringify({ participantId }),
    headers: { Authorization: `Bearer ${joinToken}` },
  });
}

/** Host only. The Room refuses the kicked participantId on a later join. */
export function kickParticipant(
  roomCode: string,
  participantId: string,
  joinToken: string,
): Promise<void> {
  const body: RoomKickRequest = { participantId };
  return request<void>(`/rooms/${encodeURIComponent(roomCode)}/kick`, {
    method: "POST",
    body: JSON.stringify(body),
    headers: { Authorization: `Bearer ${joinToken}` },
  });
}

export function updateTracks(
  roomCode: string,
  participantId: string,
  tracks: RoomTrack[],
  joinToken: string,
): Promise<void> {
  return request<void>(`/rooms/${encodeURIComponent(roomCode)}/tracks`, {
    method: "PUT",
    body: JSON.stringify({ participantId, tracks }),
    headers: { Authorization: `Bearer ${joinToken}` },
  });
}

export function setMuted(
  roomCode: string,
  participantId: string,
  muted: boolean,
  joinToken: string,
): Promise<void> {
  const body: RoomMuteUpdate = { participantId, muted };
  return request<void>(`/rooms/${encodeURIComponent(roomCode)}/mute`, {
    method: "PUT",
    body: JSON.stringify(body),
    headers: { Authorization: `Bearer ${joinToken}` },
  });
}

export function roomSocketUrl(roomCode: string, joinToken: string): string {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams({ joinToken }).toString();
  return `${protocol}//${window.location.host}${API_BASE}/rooms/${encodeURIComponent(roomCode)}/ws?${query}`;
}
