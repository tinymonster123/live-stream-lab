import { expect, test } from "@playwright/test";

test("renders the Broadcast panel and validates the file source", async ({ page }) => {
  await page.goto("/");

  await expect(page.getByRole("heading", { name: "Live Stream Lab" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Broadcast" })).toBeVisible();

  const create = page.getByRole("button", { name: "Create Broadcast" });
  await expect(create).toBeDisabled();

  await expect(page.getByLabel("Latency")).toHaveValue("low");
  await page.getByLabel("File path").fill("/tmp/fixture.mp4");
  await expect(create).toBeEnabled();
});
