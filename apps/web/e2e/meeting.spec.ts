import { expect, type Locator, test } from "@playwright/test";

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

test("two tabs publish over WHIP and subscribe over WHEP", async ({ browser }) => {
  const host = await browser.newPage();
  const guest = await browser.newPage();

  await host.goto("/");
  await host.getByLabel("Host name").fill("Alice");
  await host.getByRole("button", { name: "Create Meeting" }).click();

  const roomCode = await host.getByTestId("room-code").textContent();
  expect(roomCode).toBeTruthy();
  await expect(host.getByTestId("publish-state")).toHaveText("live");
  await expectVideoPlaying(host.getByTestId("local-video"));

  await guest.goto("/");
  await guest.getByLabel("Room code").fill(roomCode ?? "");
  await guest.getByLabel("Your name").fill("Bob");
  await guest.getByRole("button", { name: "Join Room" }).click();

  await expect(guest.getByTestId("publish-state")).toHaveText("live");
  await expectVideoPlaying(guest.getByTestId("local-video"));

  // The guest joined after the host published, so the JoinGrant already
  // carried the host's WHEP target.
  await expectRemoteMediaPlaying(guest.locator('[data-testid^="remote-video-"]').first());

  // The host learns about the guest from track.published and subscribes then.
  await expect(host.getByTestId("roster")).toContainText("Bob");
  await expectRemoteMediaPlaying(host.locator('[data-testid^="remote-video-"]').first());

  await guest.getByRole("button", { name: "Leave" }).click();
  await expect(host.locator('[data-testid^="remote-video-"]')).toHaveCount(0);

  await host.close();
  await guest.close();
});

test("two tabs share the Roster over WebSocket", async ({ browser }) => {
  const host = await browser.newPage();
  const guest = await browser.newPage();

  await host.goto("/");
  await host.getByLabel("Host name").fill("Alice");
  await host.getByRole("button", { name: "Create Meeting" }).click();

  const roomCode = await host.getByTestId("room-code").textContent();
  expect(roomCode).toBeTruthy();
  await expect(host.getByTestId("room-connection")).toHaveText("open");
  await expect(host.getByTestId("roster")).toContainText("Alice");

  await guest.goto("/");
  await guest.getByLabel("Room code").fill(roomCode ?? "");
  await guest.getByLabel("Your name").fill("Bob");
  await guest.getByRole("button", { name: "Join Room" }).click();

  await expect(guest.getByTestId("room-code")).toHaveText(roomCode ?? "");
  await expect(guest.getByTestId("room-connection")).toHaveText("open");
  await expect(guest.getByTestId("roster")).toContainText("Alice");
  await expect(host.getByTestId("roster")).toContainText("Bob");

  await guest.getByRole("button", { name: "Leave" }).click();
  await expect(guest.getByRole("button", { name: "Create Meeting" })).toBeVisible();
  await expect(host.getByTestId("roster")).not.toContainText("Bob");

  await host.close();
  await guest.close();
});
