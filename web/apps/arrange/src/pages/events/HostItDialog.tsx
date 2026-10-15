import { useState } from "react";
import { ApiError, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import {
  Button,
  Dialog,
  ErrorBox,
  Field,
  Loading,
  fmtMoney,
  useLoad,
} from "../../ui/index.ts";
import "./events.css";

/** internal/admin/hosted_pricing.go hostedPriceDTO */
interface HostedPrice {
  team_cents: number;
  currency: string;
  vat_excluded?: boolean;
}

/**
 * Host it: a hosted event of one of the org's adventures, run privately
 * from the console or a host link — game after game for 14 days from
 * the first. Made at once; each team is charged to the organisation as
 * it joins a game.
 */
export function HostItDialog({
  slug,
  title,
  onClose,
}: {
  slug: string;
  title: string;
  onClose: () => void;
}) {
  const t = useT();
  const loc = t.current();
  const price = useLoad(
    (signal) => request<HostedPrice>("/api/admin/hosted-price", { signal }),
    [],
  );
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const p = price.data;

  async function submit() {
    if (busy) return;
    setError(null);
    setBusy(true);
    try {
      const r = await request<{ event_id?: number }>(
        `/api/admin/adventures/${encodeURIComponent(slug)}/host`,
        {
          method: "POST",
          body: { name: name.trim() },
        },
      );
      if (r.event_id) {
        onClose();
        navigate({ name: "event", id: r.event_id });
      }
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
      setBusy(false);
    }
  }

  return (
    <Dialog
      title={t.t("Host it")}
      onClose={() => !busy && onClose()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t.t("Cancel")}
          </Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!p}>
            {t.t("Host it")}
          </Button>
        </>
      }
    >
      {!p ? (
        price.error ? (
          <ErrorBox
            message={price.error}
            retry={price.reload}
            retryLabel={t.t("Retry")}
          />
        ) : (
          <Loading label={t.t("Loading…")} />
        )
      ) : (
        <>
          <p className="evs-muted">
            {t.t(
              "Run {{title}} yourself, with no organiser: print the cards, share the player link, and play as many games as you like for 14 days from the first. Start the first game within 60 days.",
              { title },
            )}
          </p>
          <Field label={t.t("Name")}>
            <input
              value={name}
              maxLength={80}
              placeholder={title}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <p className="evs-muted evs-small">
            {p.team_cents > 0
              ? `${t.t("Each team costs {{price}} as it joins a game, from your organisation's balance.", {
                  price: fmtMoney(p.team_cents, p.currency, loc),
                })} ${p.vat_excluded ? t.t("Excl. VAT.") : t.t("Incl. VAT.")}`
              : t.t("Free on this server.")}
          </p>
          {error && <ErrorBox message={error} />}
        </>
      )}
    </Dialog>
  );
}
