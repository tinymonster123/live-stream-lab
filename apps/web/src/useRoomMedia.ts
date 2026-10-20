import { useCallback, useEffect, useRef, useState } from "react";
import {
  type JoinGrant,
  type RoomTrack,
  type SubscriptionTarget,
  setMuted as updateMute,
  updateTracks,
  type WsEvent,
} from "./api";
import { errorMessage } from "./errors";
import {
  openLocalMedia,
  publishWhip,
  subscribeWhep,
  type WhepSubscription,
  type WhipPublisher,
} from "./media";

export type RoomMembership = JoinGrant & {
  roomCode: string;
};

export type PublishState = "idle" | "starting" | "live" | "error";

type ActiveSubscription = {
  closed: boolean;
  session: WhepSubscription | null;
};

/** WHEP can fail while SRS finishes registering a just-published Stream. */
const subscribeAttempts = 3;
const subscribeRetryDelayMs = 500;

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, milliseconds);
  });
}

/**
 * Report only the Tracks the captured Stream actually carries. Camera off
 * leaves the video Track out while the WHIP Stream stays connected; the
 * senders keep sending black frames and silence, so no renegotiation is
 * needed to turn either back on.
 */
function reportedTracks(stream: MediaStream, cameraOn: boolean): RoomTrack[] {
  const tracks: RoomTrack[] = [];
  if (cameraOn && stream.getVideoTracks().length > 0) {
    tracks.push({ trackId: "camera", kind: "video" });
  }
  if (stream.getAudioTracks().length > 0) {
    tracks.push({ trackId: "microphone", kind: "audio" });
  }
  return tracks;
}

function stopTracks(stream: MediaStream | null) {
  stream?.getTracks().forEach((track) => {
    track.stop();
  });
}

/** Disabled Tracks keep their senders and send black frames or silence. */
function setTrackEnabled(stream: MediaStream, kind: "audio" | "video", enabled: boolean) {
  const tracks = kind === "video" ? stream.getVideoTracks() : stream.getAudioTracks();
  for (const track of tracks) {
    track.enabled = enabled;
  }
}

/**
 * Owns the local camera/microphone, the WHIP publisher, and one WHEP
 * subscription per other Participant. The control plane decides who to
 * subscribe to; this hook only opens and closes peer connections.
 */
export function useRoomMedia(membership: RoomMembership | null) {
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  const [remoteStreams, setRemoteStreams] = useState<ReadonlyMap<string, MediaStream>>(
    () => new Map(),
  );
  const [state, setState] = useState<PublishState>("idle");
  const [muted, setMutedState] = useState(false);
  const [cameraOn, setCameraOnState] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const publisherRef = useRef<WhipPublisher | null>(null);
  const localStreamRef = useRef<MediaStream | null>(null);
  const mutedRef = useRef(false);
  const cameraOnRef = useRef(true);
  const subscriptionsRef = useRef(new Map<string, ActiveSubscription>());
  // Track IDs still published by each remote Participant. One WHEP session
  // serves all of them, so it closes only when the last Track goes away.
  const remoteTracksRef = useRef(new Map<string, Set<string>>());
  const membershipRef = useRef(membership);
  membershipRef.current = membership;

  const dropSubscription = useCallback((participantId: string) => {
    const active = subscriptionsRef.current.get(participantId);
    if (!active) {
      return;
    }
    subscriptionsRef.current.delete(participantId);
    active.closed = true;
    void active.session?.close();

    setRemoteStreams((current) => {
      if (!current.has(participantId)) {
        return current;
      }
      const next = new Map(current);
      next.delete(participantId);
      return next;
    });
  }, []);

  const upsertRemote = useCallback((participantId: string, stream: MediaStream) => {
    setRemoteStreams((current) => {
      const next = new Map(current);
      next.set(participantId, stream);
      return next;
    });
  }, []);

  const subscribeTo = useCallback(
    async (target: SubscriptionTarget) => {
      const self = membershipRef.current;
      if (!self || target.participantId === self.participantId) {
        return;
      }
      if (subscriptionsRef.current.has(target.participantId)) {
        return;
      }

      // Reserve the slot before awaiting so the camera and microphone events of
      // the same Participant do not open two peer connections.
      const active: ActiveSubscription = { closed: false, session: null };
      subscriptionsRef.current.set(target.participantId, active);
      const stream = new MediaStream();

      for (let attempt = 1; attempt <= subscribeAttempts; attempt += 1) {
        try {
          const session = await subscribeWhep(target.whepUrl, (track) => {
            if (active.closed) {
              return;
            }
            stream.addTrack(track);
            upsertRemote(target.participantId, stream);
          });
          if (active.closed) {
            await session.close();
            return;
          }
          active.session = session;
          return;
        } catch (caught) {
          if (active.closed) {
            return;
          }
          if (attempt < subscribeAttempts) {
            await delay(subscribeRetryDelayMs * attempt);
            continue;
          }

          subscriptionsRef.current.delete(target.participantId);
          // A failed subscription is user-visible: that Participant's video
          // will not appear. Keep the local publisher and other subscriptions.
          setError(errorMessage(caught));
          console.warn(`WHEP subscribe to ${target.participantId} failed`, caught);
        }
      }
    },
    [upsertRemote],
  );

  const handleEvent = useCallback(
    (event: WsEvent) => {
      const self = membershipRef.current;
      if (!self) {
        return;
      }

      switch (event.type) {
        case "track.published": {
          if (event.participantId === self.participantId) {
            break;
          }
          const trackIds = remoteTracksRef.current.get(event.participantId) ?? new Set<string>();
          trackIds.add(event.trackId);
          remoteTracksRef.current.set(event.participantId, trackIds);
          void subscribeTo({
            participantId: event.participantId,
            trackId: event.trackId,
            whepUrl: event.whepUrl,
          });
          break;
        }
        case "track.unpublished": {
          const trackIds = remoteTracksRef.current.get(event.participantId);
          trackIds?.delete(event.trackId);
          if (!trackIds || trackIds.size === 0) {
            remoteTracksRef.current.delete(event.participantId);
            dropSubscription(event.participantId);
          }
          break;
        }
        case "participant.left":
          remoteTracksRef.current.delete(event.participantId);
          dropSubscription(event.participantId);
          break;
        default:
          break;
      }
    },
    [dropSubscription, subscribeTo],
  );

  const reportTracks = useCallback(async (includeCamera: boolean) => {
    const self = membershipRef.current;
    const stream = localStreamRef.current;
    if (!self || !stream) {
      return;
    }
    await updateTracks(
      self.roomCode,
      self.participantId,
      reportedTracks(stream, includeCamera),
      self.joinToken,
    );
  }, []);

  const toggleMute = useCallback(async () => {
    const self = membershipRef.current;
    const stream = localStreamRef.current;
    if (!self || !stream) {
      return;
    }

    const previous = mutedRef.current;
    const apply = (muted: boolean) => {
      mutedRef.current = muted;
      setMutedState(muted);
      setTrackEnabled(stream, "audio", !muted);
    };

    apply(!previous);
    try {
      await updateMute(self.roomCode, self.participantId, !previous, self.joinToken);
    } catch (caught) {
      apply(previous);
      setError(errorMessage(caught));
    }
  }, []);

  const toggleCamera = useCallback(async () => {
    const self = membershipRef.current;
    const stream = localStreamRef.current;
    if (!self || !stream) {
      return;
    }

    const previous = cameraOnRef.current;
    const apply = (cameraOn: boolean) => {
      cameraOnRef.current = cameraOn;
      setCameraOnState(cameraOn);
      setTrackEnabled(stream, "video", cameraOn);
    };

    apply(!previous);
    try {
      await reportTracks(!previous);
    } catch (caught) {
      // Keep the Roster and the local media in agreement: a failed report
      // would otherwise leave the camera on while others believe it is off.
      apply(previous);
      setError(errorMessage(caught));
    }
  }, [reportTracks]);

  useEffect(() => {
    if (!membership) {
      return;
    }

    const current = membership;
    let cancelled = false;
    let local: MediaStream | null = null;
    setState("starting");
    setError(null);

    async function start() {
      try {
        local = await openLocalMedia();
        if (cancelled) {
          stopTracks(local);
          return;
        }
        setLocalStream(local);
        localStreamRef.current = local;
        mutedRef.current = false;
        cameraOnRef.current = true;
        setMutedState(false);
        setCameraOnState(true);

        const publisher = await publishWhip(current.publishUrl, local);
        if (cancelled) {
          await publisher.close();
          stopTracks(local);
          return;
        }
        publisherRef.current = publisher;
        setState("live");

        // The WHIP publish succeeded; a failed Track report only means other
        // Participants will not discover this Stream. Keep the local preview
        // and subscriptions alive and surface the error.
        try {
          await reportTracks(true);
        } catch (caught) {
          if (!cancelled) {
            setError(errorMessage(caught));
          }
        }

        if (cancelled) {
          return;
        }
        for (const target of current.subscriptions) {
          await subscribeTo(target);
        }
      } catch (caught) {
        if (!cancelled) {
          setState("error");
          setError(errorMessage(caught));
        }
      }
    }

    void start();

    return () => {
      cancelled = true;
      for (const participantId of [...subscriptionsRef.current.keys()]) {
        dropSubscription(participantId);
      }
      remoteTracksRef.current.clear();

      const publisher = publisherRef.current;
      publisherRef.current = null;
      if (publisher) {
        void publisher.close();
      }
      stopTracks(local);
      localStreamRef.current = null;
      mutedRef.current = false;
      cameraOnRef.current = true;

      setLocalStream(null);
      setRemoteStreams(new Map());
      setState("idle");
      setMutedState(false);
      setCameraOnState(true);
      setError(null);
    };
  }, [membership, dropSubscription, reportTracks, subscribeTo]);

  return {
    localStream,
    remoteStreams,
    state,
    muted,
    cameraOn,
    error,
    handleEvent,
    toggleMute,
    toggleCamera,
  };
}
