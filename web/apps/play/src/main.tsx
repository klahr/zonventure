import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { I18nProvider } from "./i18n.tsx";
import "./maplibre.ts";
import "./index.css";

// The play surface is always light, as play.html's was: outdoor sunlight
// readability and QR contrast beat night comfort. Set before the first
// render so no dark frame flashes.
document.documentElement.dataset.theme = "light";

const root = document.getElementById("root");
if (!root) throw new Error("#root missing from index.html");

createRoot(root).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
);

// The worker only keeps the app shell openable offline; see public/sw.js
// for what it deliberately leaves alone. Registered after load so it
// never competes with the first paint on a slow connection.
if ("serviceWorker" in navigator && import.meta.env.PROD) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/app/play/sw.js", { scope: "/app/play/" }).catch(() => {
      /* unsupported or blocked: the app still works online */
    });
  });
}
