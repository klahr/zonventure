import { useRef, useState } from "react";
import { Copy, Eye } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Card, Field, fmtDateTime, ICON, relTime, Row, Stat, Stats, toDate, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { ConnectStatus, EventInfo, Team } from "./api.ts";
import { copyText, fmtElapsedShort, fmtPoints, members, recapURL, teamStuck } from "./util.ts";

/**
 * The three numbers that matter in this phase. Before the start: teams,
 * players joined, when it starts. During: teams done, teams stuck,
 * elapsed. After: teams finished, players who took part, how long it
 * ran. Elapsed stops at the end and leaves pauses out, as the live
 * board counts it.
 */
export function QuickStats({ event, teams, now }: { event: EventInfo; teams: Team[]; now: number }) {
  const t = useT();
  const loc = t.current();
  const done = teams.filter((tm) => tm.completed_at).length;
  let slots = 0, joined = 0;
  for (const tm of teams) for (const m of members(tm)) { slots++; if (m.last_login_at) joined++; }
  const players = <Stat value={`${joined}/${slots}`} label={t.t("players joined")} {...(slots > 0 && joined === slots ? { tone: "good" as const } : {})} />;
  const teamsDone = (label: string) => <Stat value={`${done}/${teams.length}`} label={label} {...(teams.length > 0 && done === teams.length ? { tone: "good" as const } : {})} />;

  const start = toDate(event.started_at)?.getTime();
  if (!start) {
    const at = toDate(event.scheduled_at);
    const soon = at && at.getTime() - now < 48 * 3600_000;
    return (
      <Stats>
        <Stat value={teams.length} label={teams.length === 1 ? t.t("team") : t.t("teams")} />
        {players}
        {event.state === "cancelled" ? (
          <Stat value="—" label={t.t("never started")} />
        ) : at ? (
          <Stat
            value={soon ? (at.getTime() > now ? fmtElapsedShort(Math.floor((at.getTime() - now) / 1000)) : t.t("now")) : at.toLocaleDateString(loc, { day: "numeric", month: "short" })}
            label={soon ? t.t("starts in") : t.t("starts {{when}}", { when: relTime(event.scheduled_at, loc) })}
          />
        ) : (
          <Stat value="—" label={t.t("starts when you start it")} />
        )}
      </Stats>
    );
  }
  let end = now;
  for (const ts of [event.completed_at, event.cancelled_at, event.paused_at]) {
    const ms = toDate(ts)?.getTime();
    if (ms && ms < end) end = ms;
  }
  const elapsed = fmtElapsedShort(Math.max(0, Math.floor((end - start) / 1000) - (event.paused_seconds || 0)));
  if (event.state === "completed" || event.state === "cancelled") {
    return (
      <Stats>
        {teamsDone(t.t("teams finished"))}
        {players}
        <Stat value={elapsed} label={t.t("duration")} />
      </Stats>
    );
  }
  const stuck = teams.filter((tm) => teamStuck(event, tm, now)).length;
  return (
    <Stats>
      {teamsDone(teams.length === 1 ? t.t("team done") : t.t("teams done"))}
      <Stat value={stuck} label={t.t("stuck")} {...(stuck > 0 ? { tone: "warn" as const } : {})} />
      <Stat value={elapsed} label={event.state === "paused" ? t.t("elapsed · paused") : t.t("elapsed")} />
    </Stats>
  );
}

/** The standings once the event has ended, with each team's recap link. */
export function ResultsCard({ event, teams }: { event: EventInfo; teams: Team[] }) {
  const t = useT();
  const toast = useToast();
  if (event.state !== "completed" && event.state !== "cancelled") return null;
  const locale = t.current();
  const recap = (tm: Team) =>
    tm.recap_token ? (
      <a className="ev-recap" href={recapURL(tm.recap_token)} target="_blank" rel="noopener">
        <Eye size={ICON.sm} aria-hidden />
        {t.t("Recap")}
      </a>
    ) : null;
  const ranked = teams.filter((tm) => tm.rank).sort((a, b) => (a.rank || 0) - (b.rank || 0) || a.name.localeCompare(b.name));
  const rows: { key: number; rank: string; name: string; meta: string; dnf?: boolean; link: React.ReactNode }[] = [
    ...ranked.map((tm) => ({
      key: tm.id,
      rank: "#" + tm.rank,
      name: tm.name,
      meta: t.t("{{n}} points", { n: fmtPoints(tm.score) }) + " · " + (tm.completed_at ? t.t("finished {{when}}", { when: fmtDateTime(tm.completed_at, locale) }) : t.t("did not finish")),
      link: recap(tm),
    })),
    ...teams.filter((tm) => !tm.rank).map((tm) => ({ key: tm.id, rank: "—", name: tm.name, meta: tm.withdrawn_at ? t.t("withdrew") : tm.cheated ? t.t("cheated") : t.t("did not start"), dnf: true, link: null })),
  ];
  const copyAll = async () => {
    const lines = teams.filter((tm) => tm.recap_token).map((tm) => `${tm.name}\t${recapURL(tm.recap_token!)}`);
    if (lines.length === 0) return toast.err(t.t("No recap links to copy"));
    if (await copyText(lines.join("\n"))) toast.ok(t.t("Copied {{n}} recap links", { n: lines.length }));
    else toast.err(t.t("Could not copy."));
  };
  return (
    <Card
      title={event.state === "cancelled" ? t.t("Final standings (cancelled)") : t.t("Results")}
      actions={<Button size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copyAll()}>{t.t("Copy recap links")}</Button>}
    >
      {rows.length === 0 ? (
        <p className="ev-muted">{t.t("No teams yet.")}</p>
      ) : (
        <ol className="ev-results">
          {rows.map((r) => (
            <li key={r.key} className={r.dnf ? "is-dnf" : undefined}>
              <span className="ev-results__rank">{r.rank}</span>
              <strong>{r.name}</strong>
              <span className="ev-muted">{r.meta}</span>
              {r.link}
            </li>
          ))}
        </ol>
      )}
    </Card>
  );
}

/**
 * A quest that can't sell yet: the marketplace fee is on and the org's
 * Stripe Connect account can't take charges. The seller's own admins get
 * the way to fix it; anyone else is told who can.
 */
export function ConnectBanner({ event, status, user }: { event: EventInfo; status: ConnectStatus | null; user: Whoami }) {
  const t = useT();
  if (!event.public_for_sale || !status || !status.platform_fee_enabled || status.charges_enabled) return null;
  const owner = user.role === "org_admin" && user.org_id === event.org_id;
  return (
    <Callout variant="warning">
      <strong>{t.t("Stripe Connect required")}</strong>
      <p className="ev-callout-p">
        {owner
          ? t.t("Your org hasn't completed Stripe Connect onboarding yet, so this self-guided adventure is hidden from your store and can't take payments. Set it up under Settings → Billing.")
          : t.t("The seller org hasn't completed Stripe Connect onboarding yet, so this self-guided adventure is hidden from their store and can't take payments. Their org admin needs to set it up under Settings → Billing.")}
      </p>
      {owner && <a className="btn primary sm" href={href({ name: "settings", tab: "billing" })}>{t.t("Set up Stripe Connect")}</a>}
    </Callout>
  );
}

const CURRENCIES = ["eur", "usd", "sek", "gbp", "dkk", "nok"];

/**
 * A quest's store settings: ticket price, currency, team size, blurb and
 * cover. Whether it is listed follows its state (running/paused →
 * listed), so there is no listing switch here.
 */
export function StoreCard({ event, onSaved }: { event: EventInfo; onSaved: () => void }) {
  const t = useT();
  const toast = useToast();
  const saved = event.ticket_price_cents ? (event.ticket_price_cents / 100).toFixed(2) : "";
  const [price, setPrice] = useState(saved);
  const [currency, setCurrency] = useState((event.ticket_currency || "eur").toLowerCase());
  const [size, setSize] = useState(String(event.ticket_team_size || 5));
  const [blurb, setBlurb] = useState(event.public_blurb || "");
  const [busy, setBusy] = useState(false);
  const [cover, setCover] = useState<{ has: boolean; bust: number }>({ has: !!event.has_cover_image, bust: 0 });
  const file = useRef<HTMLInputElement>(null);

  const save = async () => {
    const major = price.trim();
    const cents = major === "" ? 0 : Math.round(Number.parseFloat(major) * 100);
    if (major !== "" && (Number.isNaN(cents) || cents < 0)) return toast.err(t.t("Enter a valid price (or leave blank to clear)"));
    const n = Number.parseInt(size, 10);
    if (!Number.isFinite(n) || n < 1 || n > 50) return toast.err(t.t("Team size must be between 1 and 50"));
    setBusy(true);
    try {
      await api.patchEvent(event.id, { ticket_price_cents: cents, ticket_currency: currency, ticket_team_size: n, public_blurb: blurb.trim() });
      toast.ok(t.t("Saved"));
      onSaved();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  const upload = async (f: File) => {
    try {
      await api.uploadCover(event.id, f);
      toast.ok(t.t("Cover updated"));
      setCover({ has: true, bust: Date.now() });
    } catch (e) {
      toast.err(e);
    }
  };
  const remove = async () => {
    try {
      await api.deleteCover(event.id);
      toast.ok(t.t("Cover removed"));
      setCover({ has: false, bust: 0 });
    } catch (e) {
      toast.err(e);
    }
  };
  return (
    <Card
      title={t.t("Self-guided adventure details")}
      subtitle={saved ? t.t("{{price}} {{cur}} per ticket", { price: saved, cur: (event.ticket_currency || "eur").toUpperCase() }) : t.t("No ticket price set yet — players can't buy until you save one.")}
    >
      <Row>
        <div className="ev-cover" style={cover.has ? { backgroundImage: `url(/store-cover/event/${event.id}${cover.bust ? "?v=" + cover.bust : ""})` } : undefined} role="img" aria-label={t.t("Cover image (16:9 looks best)")} />
        <div className="ev-stack">
          <span className="ev-muted">{t.t("Cover image (16:9 looks best)")}</span>
          <Row gap="sm">
            <Button size="sm" onClick={() => file.current?.click()}>{cover.has ? t.t("Replace cover") : t.t("Upload cover")}</Button>
            {cover.has && <Button size="sm" variant="ghost" onClick={() => void remove()}>{t.t("Remove")}</Button>}
            <input ref={file} type="file" accept="image/*" hidden aria-label={t.t("Upload cover")} onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ""; if (f) void upload(f); }} />
          </Row>
        </div>
      </Row>
      <Row>
        <Field label={t.t("Price")}>
          <input type="number" step="0.01" min="0" placeholder="25.00" className="ev-w8" value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
        <Field label={t.t("Currency")}>
          <select value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {CURRENCIES.map((c) => <option key={c} value={c}>{c.toUpperCase()}</option>)}
          </select>
        </Field>
        <Field label={t.t("Team size")}>
          <input type="number" min="1" max="50" className="ev-w5" value={size} onChange={(e) => setSize(e.target.value)} />
        </Field>
      </Row>
      <Field label={t.t("Blurb (shown on the store card)")}>
        <textarea rows={2} maxLength={1000} placeholder={t.t("Two-hour treasure hunt around the old town.")} value={blurb} onChange={(e) => setBlurb(e.target.value)} />
      </Field>
      <Row>
        <Button variant="primary" size="sm" loading={busy} onClick={() => void save()}>{t.t("Save")}</Button>
        <a href="/store" target="_blank" rel="noopener">{t.t("Open store ↗")}</a>
      </Row>
    </Card>
  );
}
