import { useEffect, useState } from "react";
import { adventures, type adventures as A } from "@zonventure/api";
import { useT } from "./i18n.tsx";
import { href } from "./router.ts";

type Summary = A.AdventureSummary;

export function AdventuresPage() {
  const t = useT();
  const [list, setList] = useState<Summary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    adventures
      .listAdventures(ac.signal)
      .then((r) => setList(r.adventures))
      .catch((e: unknown) => {
        if (!ac.signal.aborted) setError(e instanceof Error ? e.message : String(e));
      });
    return () => ac.abort();
  }, []);

  if (error) return <div className="alert alert--danger" role="alert">{error}</div>;
  if (!list) return <p className="lede">{t.t("Loading…")}</p>;

  const editable = list.filter((a) => a.mine_to_edit);
  return (
    <>
      <h1>{t.t("Adventures")}</h1>
      <p className="lede">{t.t("Open an adventure to design its flow.")}</p>
      {editable.length === 0 ? (
        <p>{t.t("No adventures you can edit yet.")}</p>
      ) : (
        <ul className="adv-list">
          {editable.map((a) => (
            <li key={a.slug}>
              <a className="adv-card" href={href({ name: "designer", slug: a.slug })}>
                <span className="adv-card__title">{a.title || a.slug}</span>
                <span className="adv-card__meta">
                  {a.slug} · {t.plural(a.page_count, { one: "{{n}} page", other: "{{n}} pages" })}
                  {a.published ? "" : ` · ${t.t("draft")}`}
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
