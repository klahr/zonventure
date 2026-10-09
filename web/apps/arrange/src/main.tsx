import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { setVectorMapLoader, type VectorMapLib } from "@zonventure/play-ui";
import { App } from "./App";
import { loadMapLibre } from "./designer/ZoneMap.tsx";
import "./index.css";

// The designer's small zone maps draw from the vector style with the
// MapLibre the big maps already use.
setVectorMapLoader(() => loadMapLibre() as unknown as Promise<VectorMapLib>);

const root = document.getElementById("root");
if (!root) throw new Error("#root missing from index.html");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
