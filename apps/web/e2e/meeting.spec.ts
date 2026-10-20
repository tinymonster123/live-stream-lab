import {
  type Browser,
  type BrowserContext,
  expect,
  type Locator,
  type Page,
  test,
} from "@playwright/test";
import { frameClock } from "./fixtures/frame-clock";

type Pair = {
  hostContext: BrowserContext;
  guestContext: BrowserContext;
  host: Page;
  guest: Page;
  roomCode: string;
};

async function expectVideoPlaying(locator: Locator) {
  await expect(locator).toBeVisible({ timeout: 20_000 });
  await expect
    .poll(
      async () =>
        locator.evaluate((element) => {
          const video = element as HTMLVideoElement;
          return video.videoWidth * video.videoHeight;
        }),
      { timeout: 20_000, message: "video should decode frames" },
    )
    .toBeGreaterThan(0);
}

async function expectRemoteMediaPlaying(locator: Locator) {
  await expectVideoPlaying(locator);
  await expect
    .poll(
      async () =>
        locator.evaluate((element) => {
          const video = element as HTMLVideoElement;
          const stream = video.srcObject as MediaStream | null;
          return {
            muted: video.muted,
            paused: video.paused,
            audio: stream?.getAudioTracks().map((track) => track.readyState) ?? [],
          };
        }),
      { timeout: 20_000, message: "remote media should play video and audio" },
    )
    .toEqual({ muted: false, paused: false, audio: ["live"] });
}

/** Two isolated browser contexts, so each side has its own storage and peers. */
async function joinMeeting(browser: Browser): Promise<Pair> {
  const hostContext = await browser.newContext();
  const guestContext = await browser.newContext();
  const host = await hostContext.newPage();
  const guest = await guestContext.newPage();

  await host.goto("/");
  await host.getByLabel("Host name").fill("Alice");
  await host.getByRole("button", { name: "Create Meeting" }).click();

  const roomCode = (await host.getByTestId("room-code").textContent()) ?? "";
  expect(roomCode).toBeTruthy();
  await expect(host.getByTestId("publish-state")).toHaveText("live");

  await guest.goto("/");
  await guest.getByLabel("Room code").fill(roomCode);
  await guest.getByLabel("Your name").fill("Bob");
  await guest.getByRole("button", { name: "Join Room" }).click();
  await expect(guest.getByTestId("publish-state")).toHaveText("live");

  return { hostContext, guestContext, host, guest, roomCode };
}

async function closePair(pair: Pair) {
  await Promise.allSettled([pair.hostContext.close(), pair.guestContext.close()]);
}

/** Mean RGB value of a 32x32 downscale of the current video frame. */
async function frameBrightness(locator: Locator): Promise<number | null> {
  return locator.evaluate((element) => {
    const video = element as HTMLVideoElement;
    if (video.videoWidth === 0 || video.readyState < 2) {
      return null;
    }

    const canvas = document.createElement("canvas");
    canvas.width = 32;
    canvas.height = 32;
    const context = canvas.getContext("2d");
    if (!context) {
      return null;
    }
    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    let sum = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      sum += ((pixels[index] ?? 0) + (pixels[index + 1] ?? 0) + (pixels[index + 2] ?? 0)) / 3;
    }
    return sum / (pixels.length / 4);
  });
}

/** Peak audio amplitude over a window long enough to catch the fake beep. */
async function remoteAudioPeak(locator: Locator): Promise<number> {
  return locator.evaluate(async (element) => {
    const video = element as HTMLVideoElement;
    const stream = video.srcObject as MediaStream | null;
    if (!stream || stream.getAudioTracks().length === 0) {
      return 0;
    }

    const context = new AudioContext();
    const source = context.createMediaStreamSource(stream);
    const analyser = context.createAnalyser();
    analyser.fftSize = 1024;
    // Keep the graph pulled without monitoring the remote audio twice.
    const silence = context.createGain();
    silence.gain.value = 0;
    source.connect(analyser);
    analyser.connect(silence);
    silence.connect(context.destination);
    await context.resume();

    const data = new Float32Array(analyser.fftSize);
    const deadline = performance.now() + 600;
    let peak = 0;
    while (performance.now() < deadline) {
      await new Promise((resolve) => {
        setTimeout(resolve, 20);
      });
      analyser.getFloatTimeDomainData(data);
      for (const sample of data) {
        peak = Math.max(peak, Math.abs(sample));
      }
    }
    await context.close();
    return peak;
  });
}

/**
 * Decode the frame index painted into the current video frame. The blocks
 * encode the index MSB-first, so the publisher's local preview and the
 * subscriber's remote video can be compared as two clocks.
 */
async function readFrameClock(locator: Locator): Promise<number | null> {
  return locator.evaluate((element, clock) => {
    const video = element as HTMLVideoElement;
    if (video.videoWidth === 0 || video.readyState < 2) {
      return null;
    }

    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) {
      return null;
    }
    context.drawImage(video, 0, 0, canvas.width, canvas.height);

    let value = 0;
    const y = Math.floor(canvas.height / 2);
    for (const center of clock.bitCenters) {
      const x = Math.floor((center / clock.width) * canvas.width);
      const pixel = context.getImageData(x, y, 1, 1).data;
      const [red, green, blue] = pixel;
      const luma = 0.2126 * (red ?? 0) + 0.7152 * (green ?? 0) + 0.0722 * (blue ?? 0);
      value = value * 2 + (luma > 128 ? 1 : 0);
    }
    return value;
  }, frameClock);
}

/** One second of wall clock must advance a live video by roughly one second. */
async function expectFrameClockAdvances(locator: Locator, label: string) {
  const start = await readFrameClock(locator);
  await new Promise((resolve) => {
    setTimeout(resolve, 1000);
  });
  const end = await readFrameClock(locator);

  expect(start, `${label} frame clock is unreadable`).not.toBeNull();
  expect(end, `${label} frame clock is unreadable`).not.toBeNull();
  const advanced = ((end ?? 0) - (start ?? 0) + frameClock.frameCount) % frameClock.frameCount;
  expect(advanced, `${label} frame clock should advance at ${frameClock.fps}fps`).toBeGreaterThan(
    frameClock.fps / 2,
  );
  expect(advanced, `${label} frame clock should advance at ${frameClock.fps}fps`).toBeLessThan(
    frameClock.fps * 1.5,
  );
}

const latencySamples = 20;

/**
 * Sample the publisher's local preview and the subscriber's remote video at
 * the same moment. The frame-index difference is the pipeline latency:
 * capture, encode, SRS, jitter buffer, decode, and render. Display scanout is
 * not included, and the result is quantized to one frame and aliases only
 * beyond one full loop (~8.5s), which both clocks advancing rules out.
 */
async function measureFrameLatency(local: Locator, remote: Locator): Promise<number[]> {
  const latencies: number[] = [];
  for (let index = 0; index < latencySamples; index += 1) {
    const [localClock, remoteClock] = await Promise.all([
      readFrameClock(local),
      readFrameClock(remote),
    ]);
    if (localClock !== null && remoteClock !== null) {
      const frames = (localClock - remoteClock + frameClock.frameCount) % frameClock.frameCount;
      latencies.push((frames / frameClock.fps) * 1000);
    }
    await new Promise((resolve) => {
      setTimeout(resolve, 50);
    });
  }
  return latencies;
}

function percentile(values: number[], fraction: number): number {
  const sorted = [...values].sort((left, right) => left - right);
  const index = Math.min(sorted.length - 1, Math.floor(sorted.length * fraction));
  return sorted[index] ?? Number.POSITIVE_INFINITY;
}

test("two contexts publish and subscribe to each other", async ({ browser }) => {
  const pair = await joinMeeting(browser);
  const { host, guest } = pair;
  try {
    // The guest joined after the host published, so the JoinGrant already
    // carried the host's WHEP target.
    await expectRemoteMediaPlaying(guest.locator('[data-testid^="remote-video-"]').first());

    // The host learns about the guest from track.published and subscribes then.
    await expect(pair.host.getByTestId("roster")).toContainText("Bob");
    await expectRemoteMediaPlaying(host.locator('[data-testid^="remote-video-"]').first());
  } finally {
    await closePair(pair);
  }
});

test("mute and camera changes reach the remote view", async ({ browser }) => {
  const pair = await joinMeeting(browser);
  const { host, guest } = pair;
  try {
    const remote = host.locator('[data-testid^="remote-video-"]').first();
    await expectRemoteMediaPlaying(remote);

    const bob = host.getByTestId("roster-entry").filter({ hasText: "Bob" });
    const audible = await remoteAudioPeak(remote);
    expect(audible).toBeGreaterThan(0.01);

    // Mute is proven at the far end: the microphone Track stays published,
    // but the decoded audio goes silent.
    await guest.getByTestId("mute-toggle").click();
    await expect(guest.getByTestId("mute-toggle")).toHaveText("Unmute");
    await expect(bob).toHaveAttribute("data-muted", "true");
    await expect(bob.getByTestId("roster-muted")).toBeVisible();
    await expect
      .poll(() => remoteAudioPeak(remote), {
        timeout: 10_000,
        message: "remote audio should go silent",
      })
      .toBeLessThan(0.005);

    await guest.getByTestId("mute-toggle").click();
    await expect(bob).toHaveAttribute("data-muted", "false");
    await expect(bob.getByTestId("roster-muted")).toHaveCount(0);
    await expect
      .poll(() => remoteAudioPeak(remote), {
        timeout: 10_000,
        message: "remote audio should return",
      })
      .toBeGreaterThan(0.01);

    // Camera off sends black frames rather than stopping the Stream, so the
    // remote video stays connected but its picture goes dark.
    const lit = await frameBrightness(remote);
    expect(lit ?? 0).toBeGreaterThan(20);

    await guest.getByTestId("camera-toggle").click();
    await expect(guest.getByTestId("camera-toggle")).toHaveText("Camera On");
    await expect(bob.getByTestId("roster-camera-off")).toBeVisible();
    await expect
      .poll(async () => (await frameBrightness(remote)) ?? Number.POSITIVE_INFINITY, {
        timeout: 10_000,
        message: "remote picture should go dark",
      })
      .toBeLessThan(Math.min(25, (lit ?? 100) / 2));

    await guest.getByTestId("camera-toggle").click();
    await expect(guest.getByTestId("camera-toggle")).toHaveText("Camera Off");
    await expect
      .poll(async () => (await frameBrightness(remote)) ?? 0, {
        timeout: 10_000,
        message: "remote picture should return",
      })
      .toBeGreaterThan(20);
  } finally {
    await closePair(pair);
  }
});

test("leave and rejoin keep the same participantId", async ({ browser }) => {
  const pair = await joinMeeting(browser);
  const { host, guest } = pair;
  try {
    const participantId = await guest.getByTestId("participant-id").textContent();
    expect(participantId).toBeTruthy();

    await guest.getByRole("button", { name: "Leave" }).click();
    await expect(guest.getByTestId("rejoin")).toBeVisible();
    await expect(host.locator('[data-testid^="remote-video-"]')).toHaveCount(0);

    await guest.getByTestId("rejoin").click();
    await expect(guest.getByTestId("participant-id")).toHaveText(participantId ?? "");
    await expect(guest.getByTestId("publish-state")).toHaveText("live");
    await expectRemoteMediaPlaying(guest.locator('[data-testid^="remote-video-"]').first());
    await expect(host.getByTestId("roster")).toContainText("Bob");
    await expectRemoteMediaPlaying(host.locator('[data-testid^="remote-video-"]').first());
  } finally {
    await closePair(pair);
  }
});

test("glass-to-glass frame latency stays under 500ms", async ({ browser }, testInfo) => {
  const pair = await joinMeeting(browser);
  try {
    const local = pair.host.getByTestId("local-video");
    const remote = pair.guest.locator('[data-testid^="remote-video-"]').first();
    await expectVideoPlaying(local);
    await expectRemoteMediaPlaying(remote);

    // Validate the fixture before trusting it: one second of wall clock must
    // advance both clocks by roughly the declared frame rate. A frozen remote
    // video would otherwise still produce plausible samples.
    await expectFrameClockAdvances(local, "publisher");
    await expectFrameClockAdvances(remote, "subscriber");

    const samples = await measureFrameLatency(local, remote);
    expect(samples.length).toBeGreaterThan(10);

    const median = percentile(samples, 0.5);
    const p90 = percentile(samples, 0.9);
    await testInfo.attach("frame-latency-ms.json", {
      body: JSON.stringify({ median, p90, samples }),
      contentType: "application/json",
    });
    console.log(`frame latency: median=${median.toFixed(1)}ms p90=${p90.toFixed(1)}ms`);

    // The threshold is the product target for the ultra-low Latency Class.
    expect(median).toBeLessThan(500);
    expect(p90).toBeLessThan(500);
  } finally {
    await closePair(pair);
  }
});

test("the host kicks a Participant and their media disappears", async ({ browser }) => {
  const pair = await joinMeeting(browser);
  const { host, guest } = pair;
  try {
    await expectRemoteMediaPlaying(host.locator('[data-testid^="remote-video-"]').first());

    const bob = host.getByTestId("roster-entry").filter({ hasText: "Bob" });
    await expect(bob.getByTestId("roster-kick")).toBeVisible();
    await bob.getByTestId("roster-kick").click();

    // The Room removes the Participant and broadcasts participant.left, which
    // also releases the host's WHEP subscription.
    await expect(host.getByTestId("roster")).not.toContainText("Bob");
    await expect(host.locator('[data-testid^="remote-video-"]')).toHaveCount(0);

    // The kicked client is returned to the join form without a Rejoin offer.
    await expect(guest.getByRole("button", { name: "Create Meeting" })).toBeVisible();
    await expect(guest.getByTestId("rejoin")).toHaveCount(0);
    await expect(guest.getByRole("alert")).toContainText("removed");
  } finally {
    await closePair(pair);
  }
});

test("two contexts share the Roster over WebSocket", async ({ browser }) => {
  const pair = await joinMeeting(browser);
  const { host, guest, roomCode } = pair;
  try {
    await expect(host.getByTestId("room-connection")).toHaveText("open");
    await expect(host.getByTestId("roster")).toContainText("Alice");
    await expect(guest.getByTestId("room-code")).toHaveText(roomCode);
    await expect(guest.getByTestId("room-connection")).toHaveText("open");
    await expect(guest.getByTestId("roster")).toContainText("Alice");
    await expect(host.getByTestId("roster")).toContainText("Bob");

    await guest.getByRole("button", { name: "Leave" }).click();
    await expect(guest.getByRole("button", { name: "Create Meeting" })).toBeVisible();
    await expect(host.getByTestId("roster")).not.toContainText("Bob");
  } finally {
    await closePair(pair);
  }
});
