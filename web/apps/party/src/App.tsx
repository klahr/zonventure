import { useEffect, useRef } from "react";
import { siteName } from "@zonventure/api";
import { SPOTS } from "@zonventure/ui";
import { Book, Resend } from "./Book.tsx";
import { Host } from "./Host.tsx";
import { Join } from "./Join.tsx";
import { TrailEdit } from "./TrailEdit.tsx";
import { TrailMine, TrailPaid, TrailStart, TrailVerify } from "./TrailPages.tsx";
import { useT } from "./i18n.tsx";
import { Spot, ThankYouBanner } from "./pictures.tsx";
import { embedded, fitFrame } from "./embed.ts";

/**
 * The pages, by path: /app/party/book/{org}/{offer}, /app/party/host/{token},
 * /app/party/host/ (no token: a game summary mail's button, to send the
 * host link again), /app/party/join/{token}, and /app/party/paid, where a payment made from
 * the host page lands. The quiz trail's pages are under /app/party/trail/:
 * start/{org}, edit (#id), host (#hostToken), mine, verify (#token), paid.
 */
type Route =
  | { page: "book"; org: string; offer: string }
  | { page: "host" | "join"; token: string }
  | { page: "trail-start"; org: string }
  | { page: "paid" | "host-find" | "none" | "trail-edit" | "trail-host" | "trail-mine" | "trail-verify" | "trail-paid" };

export function route(path: string): Route {
  const parts = path.replace(/^\/app\/party\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
  if (parts[0] === "trail") {
    if (parts[1] === "start" && parts.length === 3) return { page: "trail-start", org: parts[2]! };
    const sub = { edit: "trail-edit", host: "trail-host", mine: "trail-mine", verify: "trail-verify", paid: "trail-paid" } as const;
    const page = parts.length === 2 ? sub[parts[1] as keyof typeof sub] : undefined;
    return page ? { page } : { page: "none" };
  }
  if (parts[0] === "book" && parts.length === 3) return { page: "book", org: parts[1]!, offer: parts[2]! };
  if (parts[0] === "host" && parts.length === 2) return { page: "host", token: parts[1]! };
  if (parts[0] === "host" && parts.length === 1) return { page: "host-find" };
  if (parts[0] === "join" && parts.length === 2) return { page: "join", token: parts[1]! };
  if (parts[0] === "paid" && parts.length === 1) return { page: "paid" };
  return { page: "none" };
}

export function App() {
  const i18n = useT();
  const t = i18n.t;
  const sv = i18n.current().startsWith("sv");
  const r = route(location.pathname);
  const editing = r.page === "trail-edit" || r.page === "trail-host";
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => fitFrame(root.current!, editing ? "screen" : "content"), [editing]);
  return (
    <div ref={root} className={"party" + (editing ? " party--wide" : "") + (embedded ? " party--embed" + (editing ? "" : " party--fit") : "")}>
      {!embedded && (
        <header className="party-top">
          {/* Not a link: the site's root is the console, and leaving loses the host link's page. */}
          <span className="party-brand">{siteName()}</span>
          {/* Each language by its own name; the choice sticks (zv-lang). */}
          <button type="button" className="party-link party-lang" lang={sv ? "en" : "sv"} aria-label={t("Language")} onClick={() => i18n.setLocale(sv ? "en" : "sv")}>
            {sv ? "English" : "Svenska"}
          </button>
        </header>
      )}
      <main className="party-main">
        {r.page === "book" ? (
          <Book org={r.org} offer={r.offer} />
        ) : r.page === "host" ? (
          <Host token={r.token} />
        ) : r.page === "join" ? (
          <Join token={r.token} />
        ) : r.page === "trail-start" ? (
          <TrailStart org={r.org} />
        ) : r.page === "trail-edit" || r.page === "trail-host" ? (
          <TrailEdit host={r.page === "trail-host"} />
        ) : r.page === "trail-mine" ? (
          <TrailMine />
        ) : r.page === "trail-verify" ? (
          <TrailVerify />
        ) : r.page === "trail-paid" ? (
          <TrailPaid />
        ) : r.page === "host-find" ? (
          <section className="party-card">
            <h1>{t("Open your host page")}</h1>
            <p>{t("Use the link in the email we sent you. Can't find it? We'll send a new one to the email you gave us.")}</p>
            <Resend />
          </section>
        ) : r.page === "paid" && new URLSearchParams(location.search).has("cancelled") ? (
          <section className="party-card">
            <h1>{t("The payment was cancelled. Nothing was charged.")}</h1>
            <p>{t("Close this tab and go back to your host page.")}</p>
          </section>
        ) : r.page === "paid" ? (
          <section className="party-card party-card--cover">
            <ThankYouBanner />
            <h1>{t("Thank you — payment received")}</h1>
            <p>{t("Close this tab and go back to your host page. The new teams show there in a moment.")}</p>
          </section>
        ) : (
          <section className="party-card">
            <Spot src={SPOTS.notFound} />
            <h1>{t("Nothing here")}</h1>
            <p>{t("Open the link from your email to run your party.")}</p>
          </section>
        )}
      </main>
    </div>
  );
}
