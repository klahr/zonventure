import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// The player app. Same shape as the arranger's config (see
// ../arrange/vite.config.ts): its own directory under the Go embed root,
// served from /app/play/.
//
// It is a separate Vite project on purpose. A player on a phone in a
// forest must never download a byte of the designer, and a separate
// build means the service worker's shell cache only turns over when
// player code changes.
const outDir = fileURLToPath(new URL("../../../internal/server/web/dist/play", import.meta.url));

export default defineConfig({
  base: "/app/play/",
  plugins: [react()],
  build: {
    outDir,
    emptyOutDir: true,
    manifest: true,
    modulePreload: { polyfill: false },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/ws": { target: "ws://localhost:8080", ws: true },
    },
  },
});
