import { defineConfig } from "@playwright/test";

const port = 4173;
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL,
    trace: "on-first-retry",
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
        "cd ../../apps/control-plane && go build -o /tmp/livelab-e2e-control-plane . && exec /tmp/livelab-e2e-control-plane",
      url: "http://127.0.0.1:8081/healthz",
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
