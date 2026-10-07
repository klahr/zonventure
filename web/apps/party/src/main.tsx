import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { I18nProvider, i18n } from "./i18n.tsx";
import "./index.css";

// The page that links or frames this one may say the language: ?lang=sv.
const lang = new URLSearchParams(location.search).get("lang");
if (lang && i18n.available().includes(lang)) i18n.setLocale(lang);

// Always light, as the play app: the looks paint light backdrops, and the
// join page hands over to the play app, which is light too.
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
