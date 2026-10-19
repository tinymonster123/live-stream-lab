import type { BroadcastSession, PlaybackDescriptor } from "./api";

export type BroadcastSessionStatus = BroadcastSession["status"];

export function descriptorKey(descriptor: PlaybackDescriptor): string {
  return `${descriptor.protocol}:${descriptor.url}`;
}

/**
 * Flattens the server's ordered policy into the candidates the player should
 * try. Duplicate entries are dropped.
 */
export function playbackCandidates(playback: PlaybackDescriptor): PlaybackDescriptor[] {
  const candidates: PlaybackDescriptor[] = [];
  const seen = new Set<string>();

  function visit(descriptor: PlaybackDescriptor) {
    const key = descriptorKey(descriptor);
    if (seen.has(key)) {
      return;
    }
    seen.add(key);
    candidates.push(descriptor);
    for (const fallback of descriptor.fallbacks) {
      visit(fallback);
    }
  }

  visit(playback);
  return candidates;
}

/** The visible state once playback is no longer possible. */
export function offlineMessage(status: BroadcastSessionStatus): string | null {
  switch (status) {
    case "stopped":
      return "Offline — the Broadcast has stopped.";
    case "failed":
      return "Offline — the Broadcast failed.";
    default:
      return null;
  }
}

/** Poll while the control plane can still move the Broadcast to another state. */
export function shouldPoll(status: BroadcastSessionStatus): boolean {
  return status === "starting" || status === "live" || status === "stopping";
}
