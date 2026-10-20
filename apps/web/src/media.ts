/**
 * WHIP publish and WHEP subscribe.
 *
 * WHIP and WHEP exchange a single SDP offer/answer over HTTP. There is no
 * trickle ICE: the offer travels after setLocalDescription, and the media
 * server's answer comes back in the response body. SRS is ICE-lite, so the
 * answer carries the server candidates the browser needs.
 */

export type MediaSession = {
  close: () => Promise<void>;
};

export type WhipPublisher = MediaSession & {
  stream: MediaStream;
};

export type WhepSubscription = MediaSession;

export type SdpExchange = {
  answer: string;
  /** The Location header, used to DELETE the media session. */
  resourceUrl: string | null;
};

export async function openLocalMedia(): Promise<MediaStream> {
  if (!navigator.mediaDevices?.getUserMedia) {
    throw new Error("Camera and microphone need HTTPS or localhost");
  }
  return navigator.mediaDevices.getUserMedia({ video: true, audio: true });
}

export async function publishWhip(url: string, stream: MediaStream): Promise<WhipPublisher> {
  const peer = new RTCPeerConnection();
  for (const track of stream.getTracks()) {
    peer.addTrack(track, stream);
  }

  const resourceUrl = await negotiate(peer, url);

  return {
    stream,
    close: () => closeSession(peer, resourceUrl),
  };
}

export async function subscribeWhep(
  url: string,
  onTrack: (track: MediaStreamTrack) => void,
): Promise<WhepSubscription> {
  const peer = new RTCPeerConnection();
  peer.ontrack = (event) => onTrack(event.track);
  peer.addTransceiver("audio", { direction: "recvonly" });
  peer.addTransceiver("video", { direction: "recvonly" });

  const resourceUrl = await negotiate(peer, url);

  return {
    close: () => closeSession(peer, resourceUrl),
  };
}

/** How long the browser gets to establish ICE, DTLS, and SRTP. */
const connectionTimeoutMs = 15_000;

async function negotiate(peer: RTCPeerConnection, url: string): Promise<string | null> {
  let resourceUrl: string | null = null;
  try {
    const offer = await peer.createOffer();
    await peer.setLocalDescription(offer);

    const sdp = peer.localDescription?.sdp ?? offer.sdp ?? "";
    const exchange = await postSdp(absoluteMediaUrl(url), sdp);
    resourceUrl = exchange.resourceUrl;
    await peer.setRemoteDescription({ type: "answer", sdp: exchange.answer });
    await waitForConnection(peer);
    return resourceUrl;
  } catch (error) {
    // An HTTP answer alone is not a working session; do not leak the peer or
    // the server-side session when connectivity never comes up.
    peer.close();
    void deleteSession(resourceUrl);
    throw error;
  }
}

/**
 * Resolves once DTLS/SRTP is up. The WHIP 201 only proves the SDP was
 * accepted, so callers must not report a working Stream before this resolves.
 */
export function waitForConnection(
  peer: RTCPeerConnection,
  timeoutMs = connectionTimeoutMs,
): Promise<void> {
  if (peer.connectionState === "connected") {
    return Promise.resolve();
  }

  return new Promise((resolve, reject) => {
    const finish = (error?: Error) => {
      clearTimeout(timer);
      peer.removeEventListener("connectionstatechange", onChange);
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    };
    const onChange = () => {
      if (peer.connectionState === "connected") {
        finish();
      } else if (peer.connectionState === "failed" || peer.connectionState === "closed") {
        finish(new Error(`Media connection ${peer.connectionState}`));
      }
    };
    const timer = setTimeout(() => {
      finish(new Error("Media connection timed out"));
    }, timeoutMs);

    peer.addEventListener("connectionstatechange", onChange);
  });
}

export async function postSdp(
  url: string,
  sdp: string,
  fetchImpl: typeof fetch = fetch,
): Promise<SdpExchange> {
  const response = await fetchImpl(url, {
    method: "POST",
    headers: { "Content-Type": "application/sdp" },
    body: sdp,
  });

  if (!response.ok) {
    const detail = await response.text().catch(() => "");
    throw new Error(
      `Media server rejected the SDP (HTTP ${response.status})${detail ? `: ${detail}` : ""}`,
    );
  }

  const location = response.headers.get("Location");
  return {
    answer: await response.text(),
    resourceUrl: location ? new URL(location, url).toString() : null,
  };
}

/**
 * The control plane returns /rtc/... paths so the browser stays same-origin;
 * Vite in development and Caddy on a LAN proxy them to SRS.
 */
export function absoluteMediaUrl(url: string, base = window.location.href): string {
  return new URL(url, base).toString();
}

async function closeSession(peer: RTCPeerConnection, resourceUrl: string | null): Promise<void> {
  peer.close();
  await deleteSession(resourceUrl);
}

async function deleteSession(resourceUrl: string | null): Promise<void> {
  if (!resourceUrl) {
    return;
  }
  try {
    await fetch(resourceUrl, { method: "DELETE" });
  } catch {
    // The media session is already closed locally; deleting is best effort.
  }
}
