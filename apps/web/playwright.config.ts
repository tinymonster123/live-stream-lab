import { existsSync } from "node:fs";
import { defineConfig } from "@playwright/test";
import { writeFrameClock } from "./e2e/fixtures/frame-clock";

const port = 4173;
const baseURL = `http://127.0.0.1:${port}`;

// The latency test needs frames that identify themselves. Generate the fake
// camera once per machine; every Worker shares the same launch arguments.
const frameClockPath = "/tmp/livelab-frame-clock-v1.y4m";
if (!existsSync(frameClockPath)) {
  writeFrameClock(frameClockPath);
}

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL,
    trace: "on-first-retry",
    // Deterministic camera/microphone for the WHIP publish test. The video
    // device plays a file whose frames carry their index; audio stays the
    // synthetic tone from the fake device.
    permissions: ["camera", "microphone"],
    launchOptions: {
      args: [
        "--use-fake-device-for-media-stream",
        "--use-fake-ui-for-media-stream",
        "--autoplay-policy=no-user-gesture-required",
        `--use-file-for-fake-video-capture=${frameClockPath}`,
      ],
    },
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: [
    {
      command: `pnpm exec vite --host 127.0.0.1 --port ${port} --strictPort`,
      url: baseURL,
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
    {
      // The Meeting flow needs a live control plane. Build and exec so
      // Playwright owns the server process itself; `go run` leaves a child
      // behind after Playwright stops the wrapper.
      command:
        "cd ../../apps/control-plane && go build -o /tmp/livelab-e2e-control-plane ./cmd/control-plane && exec /tmp/livelab-e2e-control-plane",
      url: "http://127.0.0.1:8081/healthz",
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      // WHIP/WHEP media needs a real SRS. The attached compose process keeps
      // the container alive for the run and stops it on teardown. SRS
      // advertises 127.0.0.1:8000/udp, which is enough for a browser on the
      // same host; LAN playback needs SRS_CANDIDATE instead.
      command: "sh ../../scripts/compose.sh up srs",
      url: "http://127.0.0.1:1985/api/v1/versions",
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
