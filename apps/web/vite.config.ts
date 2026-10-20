import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Caddy reaches Vite from the Docker network, so bind every interface.
    // Vite accepts localhost and IP Host headers by default, which covers
    // SITE_ADDRESS being an IP; add a name to allowedHosts if one is used.
    host: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8081",
        ws: true,
      },
      // WHIP/WHEP signaling stays same-origin: the control plane returns
      // /rtc/... paths and this proxy forwards them to the SRS HTTP API.
      "/rtc": {
        target: "http://127.0.0.1:1985",
      },
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
