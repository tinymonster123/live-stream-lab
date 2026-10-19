import type { components } from "../../../libs/contracts/generated/api";

export type BroadcastSession = components["schemas"]["BroadcastSession"];
export type BroadcastSource = components["schemas"]["BroadcastSource"];
export type LatencyClass = components["schemas"]["LatencyClass"];
export type PlaybackDescriptor = components["schemas"]["PlaybackDescriptor"];

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
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
