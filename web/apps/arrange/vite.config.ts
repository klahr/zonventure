import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// Each app builds into its own directory under the Go server's embed
// root, so `go build` produces one self-contained binary and the
// distroless single-container self-host story is unchanged.
//
// `base` must match the URL prefix the Go router serves this app from
// (see internal/server/spa.go). Without it, the hashed asset URLs in
// index.html would be absolute from "/" and 404 behind /app/arrange/.
const outDir = fileURLToPath(
  new URL("../../../internal/server/web/dist/arrange", import.meta.url),
);

export default defineConfig({
  base: "/app/arrange/",
  plugins: [react()],
  build: {
    outDir,
    // The committed stub lives at web/dist/index.html, one level up
    // from here, so emptying this app's own directory is safe.
    emptyOutDir: true,
    // Emitted so the Go embed test can assert that every asset
    // index.html references is actually present in the binary.
    manifest: true,
    modulePreload: { polyfill: false },
  },
  // MapLibre starts its worker as a module worker.
  worker: { format: "es" },
  server: {
    // i18n.tsx reads the classic console's catalog from internal/.
    fs: { allow: [fileURLToPath(new URL("../../..", import.meta.url))] },
    proxy: {
      "/api": "http://localhost:8080",
      "/ws": { target: "ws://localhost:8080", ws: true },
    },
  },
});
