import { siteName } from "@zonventure/api";
import { Book } from "./Book.tsx";
import { Host } from "./Host.tsx";
import { useT } from "./i18n.tsx";

/** The two pages, by path: /app/party/book/{org}/{offer} and /app/party/host/{token}. */
export function route(path: string): { page: "book"; org: string; offer: string } | { page: "host"; token: string } | { page: "none" } {
  const parts = path.replace(/^\/app\/party\/?/, "").split("/").filter(Boolean).map(decodeURIComponent);
  if (parts[0] === "book" && parts.length === 3) return { page: "book", org: parts[1]!, offer: parts[2]! };
  if (parts[0] === "host" && parts.length === 2) return { page: "host", token: parts[1]! };
  return { page: "none" };
}

export function App() {
  const t = useT().t;
  const r = route(location.pathname);
  return (
    <div className="party">
      <header className="party-top">
        <a href="/" className="party-brand">
          {siteName()}
        </a>
      </header>
      <main className="party-main">
        {r.page === "book" ? (
          <Book org={r.org} offer={r.offer} />
        ) : r.page === "host" ? (
          <Host token={r.token} />
        ) : (
          <section className="party-card">
            <h1>{t("Nothing here")}</h1>
            <p>{t("Open the link from your booking email to run your party.")}</p>
          </section>
        )}
      </main>
    </div>
  );
}
