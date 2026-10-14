import { useState } from "react";
import { Copy, Eye } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Card, Field, currencySymbol, fmtDateTime, fmtMoney, ICON, ImageDrop, ListingStatus, SubmitListingDialog, canSubmitListing, platformFeeOn, relTime, Row, Stat, Stats, toDate, useLoad, useToast } from "../../ui/index.ts";
import { useSession } from "../../session.tsx";
import * as api from "./api.ts";
import type { ConnectStatus, EventInfo, Team } from "./api.ts";
import { copyText, fmtElapsedShort, fmtPoints, joinedCount, members, recapURL, teamStuck } from "./util.ts";

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
          <Stat value="—" label={event.party ? t.t("starts when the first team joins") : t.t("starts when you start it")} />
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
  // Cancelled before it began: there are no standings or recaps to show.
  if (!event.started_at) return null;
  const locale = t.current();
  const recap = (tm: Team) =>
    tm.recap_token ? (
      <a className="ev-recap" href={recapURL(tm.recap_token)} target="_blank" rel="noopener">
        <Eye size={ICON.sm} aria-hidden />
        {t.t("Recap")}
      </a>
    ) : null;
  const ranked = teams.filter((tm) => tm.rank).sort((a, b) => (a.rank || 0) - (b.rank || 0) || a.name.localeCompare(b.name, undefined, { numeric: true }));
  const rows: { key: number; rank: string; name: string; meta: string; dnf?: boolean; link: React.ReactNode }[] = [
    ...ranked.map((tm) => ({
      key: tm.id,
      rank: "#" + tm.rank,
      name: tm.name,
      meta: t.t("{{n}} points", { n: fmtPoints(tm.score) }) + " · " + (tm.completed_at ? t.t("finished {{when}}", { when: fmtDateTime(tm.completed_at, locale) }) : joinedCount(tm) === 0 ? t.t("never joined") : t.t("did not finish")),
      link: recap(tm),
    })),
    ...teams.filter((tm) => !tm.rank).map((tm) => ({ key: tm.id, rank: "—", name: tm.name, meta: tm.withdrawn_at ? t.t("withdrew") : tm.cheated ? t.t("cheated") : joinedCount(tm) === 0 ? t.t("never joined") : t.t("did not start"), dnf: true, link: null })),
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

/**
 * A quest's store settings: ticket price, currency, team size, blurb and
 * cover. Whether it is listed follows its state (running → listed,
 * paused → not), so there is no listing switch here.
 */
/**
 * The adventure's saboteur, on or off for this event: off for a school
 * group or a team-building day where a secret betrayal would land badly.
 * Teams already dealt one when they began keep it.
 */
export function SecretRolesCard({ event, onSaved }: { event: EventInfo; onSaved: () => void }) {
  const t = useT();
  const toast = useToast();
  const [on, setOn] = useState(event.secret_roles !== false);
  const [busy, setBusy] = useState(false);
  const toggle = async (next: boolean) => {
    setOn(next);
    setBusy(true);
    try {
      await api.patchEvent(event.id, { secret_roles: next });
      toast.ok(next ? t.t("Secret roles on") : t.t("Secret roles off"));
      onSaved();
    } catch (e) {
      setOn(!next);
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t.t("Secret roles")} subtitle={t.t("This adventure has a secret role, such as a saboteur, dealt to one player on teams of three or more.")}>
      <label className="ev-check">
        <input type="checkbox" checked={on} disabled={busy} onChange={(e) => void toggle(e.target.checked)} />
        {t.t("Deal secret roles in this event")}
      </label>
      <span className="ev-muted">{t.t("Off plays the adventure without them. A team is dealt its secret roles as it makes its first move, so turning this off later leaves teams that already began as they are.")}</span>
    </Card>
  );
}

/** Where teams meet, said in their invitations and the calendar entry. */
export function MeetingPlaceCard({ event, onSaved }: { event: EventInfo; onSaved: () => void }) {
  const t = useT();
  const toast = useToast();
  const [place, setPlace] = useState(event.meeting_place || "");
  const [busy, setBusy] = useState(false);
  const save = async () => {
    setBusy(true);
    try {
      await api.patchEvent(event.id, { meeting_place: place.trim() });
      toast.ok(t.t("Saved"));
      onSaved();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title={t.t("Meeting place")} subtitle={t.t("Where teams meet before the start. Invitations and the calendar entry say it; leave it empty to give only the start's map position.")}>
      <form onSubmit={(e) => { e.preventDefault(); void save(); }}>
        <Row>
          <input value={place} maxLength={200} aria-label={t.t("Meeting place")} placeholder={t.t("e.g. By the fountain in the main square")} onChange={(e) => setPlace(e.target.value)} />
          <Button type="submit" size="sm" loading={busy} disabled={place.trim() === (event.meeting_place || "")}>{t.t("Save")}</Button>
        </Row>
      </form>
    </Card>
  );
}

/** The adventure's cheat password and how to use it, read-only: set in the adventure's details. */
export function CheatCard({ password }: { password: string }) {
  const t = useT();
  const [shown, setShown] = useState(false);
  return (
    <Card title={t.t("Testing on a phone")}>
      <Row>
        <span>{t.t("Cheat password")}:</span>
        <code>{shown ? password : "•".repeat(Math.min(password.length, 12))}</code>
        <Button size="sm" variant="ghost" onClick={() => setShown((v) => !v)}>{shown ? t.t("Hide password") : t.t("Show password")}</Button>
      </Row>
      <p className="ev-muted">{t.t("On the phone, tap the adventure's title 7 times and enter the password: every answer then counts as right, for no points, and the team is left out of the rankings. It belongs to the adventure, so every event of it shares it; change it in the adventure's details.")}</p>
    </Card>
  );
}

/** Which adventure version an event plays, and the latest published, read-only. */
export function AdventureVersion({ eventId }: { eventId: number }) {
  const t = useT();
  const v = useLoad((s) => api.getEventVersion(eventId, s), [eventId]).data;
  if (!v?.latest) return null;
  if (!v.version) return <span>{t.t("Starts on version {{n}}", { n: v.latest })}</span>;
  return <span>{v.version === v.latest ? t.t("Version {{n}}", { n: v.version }) : t.t("Version {{n}} (latest {{latest}})", { n: v.version, latest: v.latest })}</span>;
}

export function StoreCard({ event, onSaved }: { event: EventInfo; onSaved: () => void }) {
  const t = useT();
  const toast = useToast();
  const saved = event.ticket_price_cents ? (event.ticket_price_cents / 100).toFixed(2) : "";
  const [price, setPrice] = useState(saved);
  const [size, setSize] = useState(String(event.ticket_team_size || 5));
  const [blurb, setBlurb] = useState(event.public_blurb || "");
  const [busy, setBusy] = useState(false);
  const [cover, setCover] = useState<{ has: boolean; bust: number }>({ has: !!event.has_cover_image, bust: 0 });
  const [submitting, setSubmitting] = useState(false);
  const version = useLoad((s) => api.getEventVersion(event.id, s), [event.id, event.listing_review]);
  const [moving, setMoving] = useState(false);
  const [stopping, setStopping] = useState(false);
  const toggleSales = async () => {
    setStopping(true);
    try {
      await api.patchEvent(event.id, { sales_stopped: !event.sales_stopped });
      toast.ok(event.sales_stopped ? t.t("Selling again") : t.t("Sales stopped"));
      onSaved();
    } catch (e) {
      toast.err(e);
    } finally {
      setStopping(false);
    }
  };
  const update = async () => {
    setMoving(true);
    try {
      const v = await api.updateEventVersion(event.id);
      toast.ok(v.next ? t.t("Submit the listing for review to move to version {{n}}", { n: v.next }) : t.t("New teams now start on version {{n}}", { n: v.version }));
      version.reload();
      onSaved();
    } catch (e) {
      toast.err(e);
    } finally {
      setMoving(false);
    }
  };
  const v = version.data;
  // What the org keeps of a ticket once the platform fee is taken.
  const { state: session } = useSession();
  const fees = session.status === "authenticated" ? (session.user.pricing ?? {}) as Parameters<typeof platformFeeOn>[0] : {};
  const typed = Math.round(Number.parseFloat(price) * 100);
  const fee = typed > 0 ? platformFeeOn(fees, typed) : 0;
  const receive = fee > 0 ? t.t("You receive {{amount}} per ticket after the platform fee of {{fee}}.", { amount: fmtMoney(typed - fee, event.ticket_currency, t.current()), fee: fmtMoney(fee, event.ticket_currency, t.current()) }) : undefined;

  const save = async () => {
    const major = price.trim();
    const cents = major === "" ? 0 : Math.round(Number.parseFloat(major) * 100);
    if (major !== "" && (Number.isNaN(cents) || cents < 0)) return toast.err(t.t("Enter a valid price (or leave blank to clear)"));
    const n = Number.parseInt(size, 10);
    if (!Number.isFinite(n) || n < 1 || n > 50) return toast.err(t.t("Team size must be between 1 and 50"));
    setBusy(true);
    try {
      await api.patchEvent(event.id, { ticket_price_cents: cents, ticket_team_size: n, public_blurb: blurb.trim() });
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
      onSaved();
    } catch (e) {
      toast.err(e);
    }
  };
  const remove = async () => {
    try {
      await api.deleteCover(event.id);
      toast.ok(t.t("Cover removed"));
      setCover({ has: false, bust: 0 });
      onSaved();
    } catch (e) {
      toast.err(e);
    }
  };
  const perTicket = (cents: number, cur?: string) => t.t("{{price}} per ticket", { price: fmtMoney(cents, cur, t.current()) });
  // The store sells at the approved price until a changed one is approved.
  const live = event.listed && event.listed_price_cents ? event.listed_price_cents : 0;
  const subtitle = live
    ? perTicket(live, event.listed_currency) +
      (event.ticket_price_cents && event.ticket_price_cents !== live ? " · " + t.t("{{price}} after review", { price: perTicket(event.ticket_price_cents, event.ticket_currency) }) : "")
    : saved ? perTicket(event.ticket_price_cents ?? 0, event.ticket_currency) : t.t("No ticket price set yet — players can't buy until you save one.");
  // "Live in the store" only when it is: paused, stopped and unbuyable listings aren't.
  const outOfStore = event.state === "paused" || event.sales_stopped || !!event.store_blocked;
  return (
    <Card title={t.t("Self-guided adventure details")} subtitle={subtitle}>
      {event.state === "paused" && event.listed && <Callout variant="warning">{t.t("Paused: not in the store until you resume.")}</Callout>}
      {!(outOfStore && event.listing_review === "approved") && (
        <ListingStatus kind="store" review={event.listing_review} reason={event.listing_reject_reason} listed={event.listed} />
      )}
      {event.listed && !event.sales_stopped && event.store_blocked === "connect_required" && (
        <Callout variant="danger">{t.t("Not in the store: with the platform fee, tickets are sold through your own Stripe account. Connect one under Settings → Billing and it is back in the store.")}</Callout>
      )}
      {event.listed && !event.sales_stopped && event.store_blocked === "no_payments" && (
        <Callout variant="danger">{t.t("Not in the store: this server takes no payments, so nobody could buy a ticket. Ask the server's operator to set up payments.")}</Callout>
      )}
      {event.sales_stopped && <Callout variant="warning">{t.t("Sales stopped: not in the store and nobody can buy a ticket. Teams that have one play on.")}</Callout>}
      <Row>
        <Button size="sm" loading={stopping} onClick={() => void toggleSales()}>{event.sales_stopped ? t.t("Resume selling") : t.t("Stop selling")}</Button>
        {!event.sales_stopped && <span className="ev-muted">{t.t("Takes it out of the store without pausing the teams that are playing.")}</span>}
      </Row>
      {v?.can_update && (
        <Callout variant="info">
          <Row>
            <span>
              {event.listed
                ? t.t("Version {{latest}} of the adventure is published; new teams of this event start on version {{version}}.", { latest: v.latest, version: v.version })
                : t.t("Version {{latest}} of the adventure is published; this listing is still on version {{version}}.", { latest: v.latest, version: v.version })}
            </span>
            <Button size="sm" loading={moving} onClick={() => void update()}>{t.t("Update to version {{n}}", { n: v.latest })}</Button>
          </Row>
          {v.reviewed && <span className="ev-muted">{t.t("It goes through review like any change to the listing: once it is approved, new teams and teams that haven't made a move yet play it. Teams already playing keep their version.")}</span>}
        </Callout>
      )}
      {v?.next ? (
        <Callout variant="info">
          {t.t("Moves to version {{n}} once the listing is approved. Until then new teams start on version {{version}}.", { n: v.next, version: v.version })}
        </Callout>
      ) : null}
      {canSubmitListing(event.listing_review) && (
        <Row>
          <Button variant="primary" size="sm" onClick={() => setSubmitting(true)}>{t.t("Submit for review")}</Button>
          <span className="ev-muted">{t.t("Save your changes first: what is saved is what is submitted.")}</span>
        </Row>
      )}
      {submitting && (
        <SubmitListingDialog url={api.submitListingURL(event.id)} title={event.name} onClose={() => setSubmitting(false)} onDone={onSaved} />
      )}
      <Row>
        <div className="ev-stack">
          <span className="ev-muted">{t.t("Cover image (16:9 looks best)")}</span>
          <ImageDrop
            shape="cover"
            src={cover.has ? `/store-cover/event/${event.id}?draft=1${cover.bust ? "&v=" + cover.bust : ""}` : undefined}
            label={t.t("Upload cover")}
            title={t.t("Click to upload a cover")}
            accept="image/*"
            onFile={(f) => void upload(f)}
            onRemove={() => void remove()}
          />
        </div>
      </Row>
      <Row>
        <Field label={t.t("Ticket price ({{cur}})", { cur: currencySymbol(t.current()) })} hint={receive}>
          <input type="number" step="0.01" min="0" placeholder="199" className="ev-w8" value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
        <Field label={t.t("Players per ticket")}>
          <input type="number" min="1" max="50" className="ev-w5" value={size} onChange={(e) => setSize(e.target.value)} />
        </Field>
      </Row>
      <Field label={t.t("Public description")}>
        <textarea rows={2} maxLength={1000} placeholder={t.t("Two-hour treasure hunt around the old town.")} value={blurb} onChange={(e) => setBlurb(e.target.value)} />
      </Field>
      <Row>
        <Button variant="primary" size="sm" loading={busy} onClick={() => void save()}>{t.t("Save")}</Button>
        {/* Its own page in the organisation's store; the short link leads there. */}
        <a href={`/store/event/${event.id}`} target="_blank" rel="noopener">{t.t("Open store ↗")}</a>
      </Row>
    </Card>
  );
}
