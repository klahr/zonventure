import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// The party app: booking a party with no account, and the host page the
// booker runs it from. Its own build, like the player app's (see
// ../play/vite.config.ts): a parent on a phone downloads neither the
// console nor the game.
const outDir = fileURLToPath(new URL("../../../internal/server/web/dist/party", import.meta.url));

export default defineConfig({
  base: "/app/party/",
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
    },
  },
});
