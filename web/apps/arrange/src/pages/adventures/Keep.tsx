import { useState } from "react";
import { ApiError, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import { Button, Card, ErrorBox, Loading, Page, PageHeader, Row, useLoad } from "../../ui/index.ts";

interface Kept {
  title: string;
  cards: number;
  email: string;
}

/**
 * Keep it: a quiz trail made at the public door moves into the signed-in
 * organisation, from the link its buyer was mailed (or got on its admin
 * page). Its games end and its admin page goes.
 */
export function KeepPage({ token }: { token: string }) {
  const t = useT();
  const load = useLoad((signal) => request<Kept>(`/api/trail/keep/${encodeURIComponent(token)}`, { signal }), [token]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (load.error && !load.data) {
    return (
      <Page>
        <PageHeader title={t.t("Keep a party adventure")} />
        <Card>
          <p>{t.t("This link has expired or was used already. A party adventure can be kept until its days of play are over.")}</p>
        </Card>
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
  const keep = async () => {
    setBusy(true);
    setError(null);
    try {
      const r = await request<{ slug: string }>("/api/admin/trail-keep", { method: "POST", body: { token } });
      navigate({ name: "adventure", slug: r.slug });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setBusy(false);
    }
  };
  return (
    <Page>
      <PageHeader title={t.t("Keep {{title}}", { title: load.data.title })} />
      <Card>
        <p>{t.t("The party adventure and its {{n}} cards become an adventure of your organisation, to edit, print and host whenever you like.", { n: load.data.cards })}</p>
        <p>{t.t("Its host page and the games played so far end now. Their summaries were mailed to {{email}}.", { email: load.data.email })}</p>
        {error && <ErrorBox message={error} />}
        <Row>
          <Button variant="primary" filled loading={busy} onClick={() => void keep()}>
            {t.t("Keep it")}
          </Button>
        </Row>
      </Card>
    </Page>
  );
}
