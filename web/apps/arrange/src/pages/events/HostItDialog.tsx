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

/** internal/admin/hosted_pricing.go hostedPriceDTO */
interface HostedPrice {
  block_cents: number;
  currency: string;
  teams_per_block: number;
  free_teams: number;
}

const MAX_BLOCKS = 10;

/**
 * Host it: a hosted event of one of the org's adventures, run privately
 * from the console or a host link — game after game for 14 days from
 * the first. Paid at the platform's pass price per block of teams, or
 * made at once where hosting is free.
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
  const [more, setMore] = useState(false);
  const [blocks, setBlocks] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const p = price.data;
  // No blocks: free, for up to free_teams teams a game.
  const n = more ? blocks : 0;
  const cost = p ? p.block_cents * n : 0;

  async function submit() {
    if (busy) return;
    setError(null);
    setBusy(true);
    try {
      const r = await request<{ checkout_url?: string; event_id?: number }>(
        `/api/admin/adventures/${encodeURIComponent(slug)}/host`,
        {
          method: "POST",
          body: { name: name.trim(), blocks: n },
        },
      );
      if (r.checkout_url) location.assign(r.checkout_url);
      else if (r.event_id) {
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
            {p && cost > 0
              ? t.t("Pay {{price}}", { price: fmtMoney(cost, p.currency, loc) })
              : t.t("Host it")}
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
          <label className="evs-check">
            <input
              type="checkbox"
              checked={more}
              onChange={(e) => setMore(e.target.checked)}
            />
            <span>
              {t.t("More than {{n}} teams in one game?", { n: p.free_teams })}
            </span>
          </label>
          {more && (
            <Field label={t.t("Teams per game")}>
              <select
                value={blocks}
                onChange={(e) => setBlocks(Number(e.target.value))}
              >
                {Array.from({ length: MAX_BLOCKS }, (_, i) => i + 1).map(
                  (b) => (
                    <option key={b} value={b}>
                      {t.t("Up to {{n}} teams", { n: b * p.teams_per_block })}
                      {p.block_cents > 0
                        ? ` · ${fmtMoney(p.block_cents * b, p.currency, loc)}`
                        : ""}
                    </option>
                  ),
                )}
              </select>
            </Field>
          )}
          <p className="evs-muted evs-small">
            {n === 0
              ? t.t("Free for up to {{n}} teams per game.", { n: p.free_teams })
              : p.block_cents > 0
                ? t.t("{{price}} for up to {{n}} teams per game.", {
                    price: fmtMoney(cost, p.currency, loc),
                    n: n * p.teams_per_block,
                  })
                : t.t("Free on this server, for up to {{n}} teams per game.", {
                    n: n * p.teams_per_block,
                  })}
          </p>
          {error && <ErrorBox message={error} />}
        </>
      )}
    </Dialog>
  );
}
