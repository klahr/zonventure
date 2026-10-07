import { useMemo, useState } from "react";
import { ApiError, request } from "@zonventure/api";
import { Editor, problems, useTrailSave, type TrailApi, type TrailDoc, type TrailTheme } from "@zonventure/trail";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { useTopBarTitle } from "../../topbar.tsx";
import { ErrorBox, Loading, Page, useLoad, useToast } from "../../ui/index.ts";
import "@zonventure/trail/trail.css";
import "./trail-page.css";

const base = (slug: string) => `/api/admin/adventures/${encodeURIComponent(slug)}/cards`;

/**
 * A quiz trail in the simple creator: its cards, saved as they change.
 * "Done" publishes them — when every card is finished — and goes back to
 * the adventure's page.
 */
export function TrailPage({ slug }: { slug: string }) {
  const t = useT();
  const load = useLoad((signal) => request<TrailDoc>(base(slug), { signal }), [slug]);
  useTopBarTitle(load.data?.title ?? null);
  if (load.error && !load.data) {
    return (
      <Page>
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      </Page>
    );
  }
  if (!load.data) {
    return (
      <Page>
        <Loading label={t.t("Loading…")} />
      </Page>
    );
  }
  return <TrailEditor key={load.data.revision} slug={slug} doc={load.data} reload={load.reload} />;
}

function TrailEditor({ slug, doc, reload }: { slug: string; doc: TrailDoc; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const api = useMemo<TrailApi>(() => ({ save: (body) => request<TrailDoc>(base(slug), { method: "PUT", body }) }), [slug]);
  const s = useTrailSave(api, doc);
  const [focus, setFocus] = useState<{ id: string; nonce: number } | null>(null);
  const [tried, setTried] = useState(false);
  const [theme, setTheme] = useState(doc.theme);
  const pickTheme = async (th: TrailTheme) => {
    const was = theme;
    setTheme(th.id);
    try {
      await request(`${base(slug)}/theme`, { method: "PUT", body: { id: th.id, look: th.look } });
    } catch (e) {
      setTheme(was);
      toast.err(e instanceof ApiError ? e.message : e);
    }
  };
  const [busy, setBusy] = useState(false);

  const done = async () => {
    setBusy(true);
    try {
      if (!(await s.flush())) return;
      const first = problems(s.cards)[0];
      setTried(true);
      if (first) {
        if (first.card) setFocus({ id: first.card, nonce: Date.now() });
        toast.err(first.card ? t.t("Finish the marked cards first.") : t.t("Add at least one question first."));
        return;
      }
      await request(`${base(slug)}/publish`, { method: "POST" });
      navigate({ name: "adventure", slug });
    } catch (e) {
      toast.err(e instanceof ApiError ? e.message : e);
    } finally {
      setBusy(false);
    }
  };

  const pdf = `${base(slug)}.pdf`;
  return (
    <div className="trail-page">
      <Editor
        i18n={t}
        title={s.title}
        cards={s.cards}
        state={s.state}
        readOnly={s.readOnly}
        onChange={s.change}
        focus={focus}
        showProblems={tried}
        theme={theme}
        onTheme={(th) => void pickTheme(th)}
        actions={
          <>
            {s.state === "conflict" && (
              <button type="button" className="trail-btn" onClick={reload}>
                {t.t("Reload")}
              </button>
            )}
            {s.state === "designer" ? (
              <a className="trail-btn" href={href({ name: "designer", slug })}>
                {t.t("Open designer")}
              </a>
            ) : (
              <>
                <a className="trail-btn" href={pdf} download onClick={() => void s.flush()}>
                  {s.doc.reprint ? t.t("Print again") : t.t("Print cards")}
                </a>
                <a className="trail-btn" href={`${pdf}?answers=1`} download onClick={() => void s.flush()}>
                  {t.t("Answer key")}
                </a>
                <button type="button" className="trail-btn trail-btn--primary" disabled={busy} onClick={() => void done()}>
                  {t.t("Done")}
                </button>
              </>
            )}
          </>
        }
      />
    </div>
  );
}
