import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { I18nProvider, i18n } from "./i18n.tsx";
import "./index.css";

// The page that links or frames this one may say the language: ?lang=sv.
const lang = new URLSearchParams(location.search).get("lang");
if (lang && i18n.available().includes(lang)) i18n.setLocale(lang);

const root = document.getElementById("root");
if (!root) throw new Error("#root missing from index.html");

createRoot(root).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
);
