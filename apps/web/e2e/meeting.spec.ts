import { expect, test } from "@playwright/test";

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
