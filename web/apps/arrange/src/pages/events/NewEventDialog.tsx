import { useEffect, useRef, useState } from "react";
import { ApiError, canAuthor } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { CATALOG_ENABLED, navigate } from "../../router.ts";
import { Button, Callout, Dialog, Empty, ErrorBox, Field, Loading, Row, fmtMoney, useLoad, useToast, errorText } from "../../ui/index.ts";
import { activeMembership, createEvent, createPurchase, createTeam, listAdventures, listAssignments, pricing, type AdventureRow, type Assignment } from "./api.ts";
import { AdventureCover } from "./parts.tsx";
import "./events.css";

/**
 * Schedule an event: pick the adventure (unless given), name it, start
 * manually or at a time, optionally add teams — buying an event pass
 * first when one is needed. Opened from the dashboard, the events list,
 * an adventure's page and the catalog. On success it navigates to the
 * new event.
 *
 * Props are the contract other areas code against:
 *   slug          — preselect this adventure
 *   assignmentId  — use this event pass
 *   ownsIt        — the adventure is the org's own (buy-before-create flow)
 */
export interface NewEventProps {
  slug?: string;
  assignmentId?: number;
  ownsIt?: boolean;
  onClose: () => void;
}

/** The free tier's floor: a smaller pass costs the same and only caps the event. */
const MIN_PLAYERS = 15;

/**
 * What was typed before a Stripe checkout, so the dialog that reopens
 * on the way back (useJustPurchased) starts filled in rather than empty.
 */
const DRAFT_KEY = "zv-new-event-draft";
interface Draft {
  name: string;
  later: boolean;
  when: string;
  teamCount: string;
  perTeam: string;
  teamPrefix: string;
  playerPrefix: string;
}
function peekDraft(): Partial<Draft> {
  try {
    const raw = sessionStorage.getItem(DRAFT_KEY);
    return raw ? (JSON.parse(raw) as Partial<Draft>) : {};
  } catch {
    return {};
  }
}

/**
 * How the dialog gets the event its pass: an existing one, a share the
 * platform commissioned for the org (no pass needed), or a purchase
 * made on submit.
 */
type Ctx = { mode: "preset"; assignmentId: number } | { mode: "share"; slug: string } | { mode: "buy"; slug: string };

export function NewEventDialog(props: NewEventProps) {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const unverified = !!user && user.email_verified === false;

  // The server refuses unverified accounts; say so before the user
  // fills in a form whose input the refusal would throw away.
  useEffect(() => {
    if (!user || !unverified) return;
    toast.err(user.email ? t.t("Verify your email before scheduling events — check your inbox for the link.") : t.t("Add and verify an email address before scheduling events."));
    if (!user.email) navigate({ name: "settings", tab: "account" });
    props.onClose();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!user || unverified) return null;
  return <NewEventForm {...props} />;
}

function NewEventForm({ slug, assignmentId, ownsIt, onClose }: NewEventProps) {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null!;
  const loc = t.current();
  const price = pricing(user);
  const trusted = !!activeMembership(user)?.trusted;
  const needsPicker = assignmentId === undefined && slug === undefined;

  const lists = useLoad(async (signal) => {
    const [all, assigns] = await Promise.all([listAdventures(signal), listAssignments(signal)]);
    const unused = assigns.filter((a) => !a.event_id);
    // With the catalog off, only what the Adventures page shows: the
    // org's own, the commissioned, and those it holds a pass for.
    const advs = CATALOG_ENABLED ? all : all.filter((a) => a.mine_to_edit || a.shared || assigns.some((x) => x.adventure_slug === a.slug));
    return { advs, unused };
  }, []);

  // Only the dialog reopened with the pass just paid for takes the draft.
  const [draft] = useState<Partial<Draft>>(() => (assignmentId !== undefined ? peekDraft() : {}));
  useEffect(() => {
    try {
      sessionStorage.removeItem(DRAFT_KEY);
    } catch {
      /* nothing stored */
    }
  }, []);
  const [picked, setPicked] = useState<string | null>(slug ?? null);
  const [filter, setFilter] = useState("");
  const [players, setPlayers] = useState(String(MIN_PLAYERS));
  const [name, setName] = useState(draft.name ?? "");
  const [later, setLater] = useState(draft.later ?? false);
  const [when, setWhen] = useState(draft.when ?? "");
  const [teamsOpen, setTeamsOpen] = useState(Number(draft.teamCount) > 0);
  const [teamCount, setTeamCount] = useState(draft.teamCount ?? "0");
  const [perTeam, setPerTeam] = useState(draft.perTeam ?? "3");
  const [teamPrefix, setTeamPrefix] = useState(draft.teamPrefix ?? "Team");
  const [playerPrefix, setPlayerPrefix] = useState(draft.playerPrefix ?? "Player");
  const [attest, setAttest] = useState(false);
  const [attestErr, setAttestErr] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // A pass bought on an earlier submit whose event then failed: the
  // retry uses it rather than buying a second one.
  const [bought, setBought] = useState<number | null>(null);
  const whenRef = useRef<HTMLInputElement>(null);
  const attestRef = useRef<HTMLInputElement>(null);
  const teamCountRef = useRef<HTMLInputElement>(null);
  const errorRef = useRef<HTMLDivElement>(null);
  // The error sits under the form; bring it into the dialog's view.
  useEffect(() => {
    if (error) errorRef.current?.scrollIntoView({ block: "nearest" });
  }, [error]);

  const advs = lists.data?.advs ?? [];
  const unused = lists.data?.unused ?? [];

  // Picker mode starts on the first adventure, as the classic console does.
  useEffect(() => {
    if (!needsPicker || !lists.data) return;
    if (lists.data.advs.length === 0) return;
    setPicked((p) => p ?? lists.data!.advs[0]!.slug);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lists.data]);

  useEffect(() => {
    if (later) whenRef.current?.focus();
  }, [later]);

  const pickedAdv = advs.find((a) => a.slug === picked);

  const ctx: Ctx | null = (() => {
    if (bought) return { mode: "preset", assignmentId: bought };
    if (assignmentId !== undefined) return { mode: "preset", assignmentId };
    if (needsPicker) {
      if (!pickedAdv) return null;
      const pass = unused.find((x) => x.adventure_slug === pickedAdv.slug);
      if (pass) return { mode: "preset", assignmentId: pass.id };
      if (pickedAdv.shared) return { mode: "share", slug: pickedAdv.slug };
      return { mode: "buy", slug: pickedAdv.slug };
    }
    if (ownsIt) return { mode: "buy", slug: slug! };
    return { mode: "share", slug: slug! };
  })();

  const buying = ctx?.mode === "buy";
  const playersN = Math.max(0, parseInt(players, 10) || 0);
  const pass: Assignment | undefined = ctx?.mode === "preset" ? unused.find((x) => x.id === ctx.assignmentId) : undefined;
  const cap = buying ? playersN : pass?.max_players ?? 0;
  const teamN = teamsOpen ? Math.max(0, parseInt(teamCount, 10) || 0) : 0;
  const perN = Math.max(1, parseInt(perTeam, 10) || 0);
  const overCap = cap > 0 && teamN * perN > cap;
  const total = price.chargeable(Math.max(1, playersN));
  const toCheckout = buying && total > 0;

  const localTZ = (() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
    } catch {
      return "";
    }
  })();

  const adventureTitle = pickedAdv?.title ?? pass?.adventure_title ?? advs.find((a) => a.slug === (pass?.adventure_slug ?? slug))?.title ?? pass?.adventure_slug ?? slug ?? "";

  async function submit(provider: "invoice" | null) {
    if (busy || !ctx) return;
    setError(null);
    if (!attest) {
      setAttestErr(true);
      attestRef.current?.focus();
      return;
    }
    if (ctx.mode === "buy" && playersN < MIN_PLAYERS) {
      setError(t.t("Minimum {{n}} players per event", { n: MIN_PLAYERS }));
      return;
    }
    // Teams made up front must fit the event pass, or the extra
    // ones fail one by one after the event already exists.
    if (overCap) {
      setTeamsOpen(true);
      teamCountRef.current?.focus();
      setError(t.t("{{teams}} teams of {{per}} is {{total}} players, but the event pass covers {{cap}}.", { teams: teamN, per: perN, total: teamN * perN, cap }));
      return;
    }
    let scheduledAt: string | undefined;
    if (later) {
      if (!when) {
        setError(t.t("Pick an auto-start time"));
        whenRef.current?.focus();
        return;
      }
      const d = new Date(when);
      if (Number.isNaN(d.getTime())) {
        setError(t.t("Bad date"));
        return;
      }
      scheduledAt = d.toISOString();
    }

    setBusy(true);
    let passId: number | undefined = ctx.mode === "preset" ? ctx.assignmentId : undefined;
    if (ctx.mode === "buy") {
      try {
        const pr = await createPurchase({ adventure_slug: ctx.slug, max_players: playersN, ...(provider ? { provider } : {}) });
        if (pr.state === "paid" && pr.assignment_id) {
          passId = pr.assignment_id;
          setBought(pr.assignment_id);
          if (pr.provider === "invoice") toast.ok(t.t("Players added to your next invoice"));
        } else if (pr.checkout_url) {
          try {
            const d: Draft = { name, later, when, teamCount: teamsOpen ? teamCount : "0", perTeam, teamPrefix, playerPrefix };
            sessionStorage.setItem(DRAFT_KEY, JSON.stringify(d));
          } catch {
            /* the form is simply empty on the way back */
          }
          location.href = pr.checkout_url;
          return;
        } else {
          setError(t.t("Payment did not complete"));
          setBusy(false);
          return;
        }
      } catch (e) {
        setError(errorText(e) || t.t("Purchase failed"));
        setBusy(false);
        return;
      }
    }

    let id: number;
    try {
      const body = {
        name: name.trim(),
        attest_no_minors: true as const,
        ...(passId ? { assignment_id: passId } : ctx.mode === "share" ? { adventure_slug: ctx.slug } : {}),
        ...(scheduledAt ? { scheduled_at: scheduledAt } : {}),
      };
      id = (await createEvent(body)).id;
    } catch (e) {
      setError(e instanceof ApiError && e.code === "verification_required" ? t.t("Verify your email before scheduling events — check your inbox for the link.") : errorText(e));
      setBusy(false);
      return;
    }

    // Best-effort: a team that fails leaves the event in place, and
    // the event page is where it gets finished by hand.
    let failed = 0;
    const tp = teamPrefix.trim() || "Team";
    const pp = playerPrefix.trim() || "Player";
    for (let i = 1; i <= teamN; i++) {
      try {
        await createTeam(id, `${tp} ${i}`, Array.from({ length: perN }, (_, j) => `${pp} ${j + 1}`));
      } catch {
        failed++;
      }
    }
    if (failed > 0) toast.err(t.plural(failed, { one: "{{n}} team could not be added — add it on the event page.", other: "{{n}} teams could not be added — add them on the event page." }));
    toast.ok(t.t("Event ready"));
    onClose();
    navigate({ name: "event", id });
  }

  if (lists.loading && !lists.data) {
    return (
      <Dialog title={t.t("New event")} onClose={onClose}>
        <Loading label={t.t("Loading…")} />
      </Dialog>
    );
  }
  // Nothing to schedule yet: say so here, with the way forward, rather
  // than flashing an error and closing.
  if (needsPicker && lists.data && advs.length === 0) {
    const author = canAuthor(user);
    return (
      <Dialog
        title={t.t("New event")}
        onClose={onClose}
        footer={
          <>
            <Button onClick={onClose}>{t.t("Cancel")}</Button>
            {author && (
              <Button
                variant="primary"
                onClick={() => {
                  onClose();
                  navigate({ name: "adventures" });
                }}
              >
                {t.t("Open your adventures")}
              </Button>
            )}
          </>
        }
      >
        <Empty title={t.t("No adventures you can use yet.")}>
          {author ? t.t("Create your own adventure to get started.") : t.t("An organisation admin can create one for you.")}
        </Empty>
      </Dialog>
    );
  }
  if (lists.error && !lists.data) {
    return (
      <Dialog title={t.t("New event")} onClose={onClose} footer={<Button onClick={onClose}>{t.t("Cancel")}</Button>}>
        <ErrorBox message={lists.error} retry={lists.reload} retryLabel={t.t("Retry")} />
      </Dialog>
    );
  }

  // One adventure to choose from is no choice: show it as chosen.
  const picker = needsPicker && advs.length > 1;
  const shown = filter ? advs.filter((a) => (a.title + " " + a.slug).toLowerCase().includes(filter.toLowerCase())) : advs;

  return (
    <Dialog
      title={t.t("New event")}
      wide={picker}
      onClose={() => !busy && onClose()}
      onSubmit={() => void submit(null)}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>{t.t("Cancel")}</Button>
          {trusted && buying && (
            <Button onClick={() => void submit("invoice")} disabled={busy} title={t.t("Skip Stripe — your finance team will get an invoice from us.")}>
              {t.t("Add to invoice")}
            </Button>
          )}
          <Button type="submit" variant="primary" loading={busy} disabled={!ctx}>
            {toCheckout ? t.t("Continue to payment") : t.t("Create event")}
          </Button>
        </>
      }
    >
      {picker ? (
        <fieldset className="evs-fieldset">
          <legend className="cs-field__label">{t.t("Adventure")}</legend>
          {advs.length > 8 && (
            <input type="search" className="evs-filter" placeholder={t.t("Filter adventures")} aria-label={t.t("Filter adventures")} value={filter} onChange={(e) => setFilter(e.target.value)} />
          )}
          <div className="evs-picker" role="radiogroup" aria-label={t.t("Adventure")}>
            {shown.map((a) => (
              <AdventureChoice key={a.slug} adv={a} checked={a.slug === picked} pass={unused.find((x) => x.adventure_slug === a.slug)} onPick={() => { setPicked(a.slug); setBought(null); }} />
            ))}
            {shown.length === 0 && <p className="evs-muted evs-small">{t.t("No adventures match.")}</p>}
          </div>
        </fieldset>
      ) : (
        adventureTitle && (
          <div className="evs-chosen">
            <AdventureCover title={adventureTitle} src={advs.find((a) => a.slug === (pickedAdv?.slug ?? pass?.adventure_slug ?? slug))?.avatar_url} size={36} />
            <span className="evs-stack">
              <strong>{adventureTitle}</strong>
              {pass ? (
                <span className="evs-muted evs-small">{t.t("Event pass for {{n}} players", { n: pass.max_players })}</span>
              ) : needsPicker && pickedAdv && (pickedAdv.shared || !pickedAdv.playable) && (
                <span className="evs-muted evs-small">
                  {[pickedAdv.shared ? t.t("Commissioned for you") : null, !pickedAdv.playable ? t.t("not published yet") : null].filter(Boolean).join(" · ")}
                </span>
              )}
            </span>
          </div>
        )
      )}

      {ctx?.mode === "preset" && picker && pass && (
        <Callout variant="success">{t.t("Uses your unused event pass for {{n}} players.", { n: pass.max_players })}</Callout>
      )}

      {buying && (
        <div className="evs-capacity">
          <Row>
            <Field label={t.t("Players")}>
              <input type="number" min={MIN_PLAYERS} max={500} value={players} onChange={(e) => setPlayers(e.target.value)} className="evs-num" />
            </Field>
            <p className="evs-total" aria-live="polite">
              {t.t("Total: ")}
              <strong>{fmtMoney(total, price.currency, loc)}</strong>{" "}
              <span className="evs-muted evs-small">{t.t("(first {{n}} free, then {{price}} per player)", { n: price.free, price: fmtMoney(price.priceCents, price.currency, loc) })}</span>
            </p>
          </Row>
          <p className="evs-muted evs-small">
            {t.t("You can grow this later from the event page.")}
            {toCheckout && <> {t.t("You'll pay on Stripe, then come back here to finish the event.")}</>}
          </p>
        </div>
      )}

      <Field label={t.t("Name (optional)")}>
        <input value={name} maxLength={200} onChange={(e) => setName(e.target.value)} placeholder={t.t("e.g. “Saturday Andersson party”")} />
      </Field>

      <fieldset className="evs-fieldset">
        <legend className="cs-field__label">{t.t("When")}</legend>
        <label className="evs-check">
          <input type="radio" name="evs-when" checked={!later} onChange={() => setLater(false)} />
          <span>{t.t("Start manually — I'll hit Start when we're ready")}</span>
        </label>
        <label className="evs-check">
          <input type="radio" name="evs-when" checked={later} onChange={() => setLater(true)} />
          <span>{t.t("Schedule for later")}</span>
        </label>
        {later && (
          <Field label={t.t("Auto-start at")} hint={localTZ ? t.t("Times are interpreted as your local timezone ({{tz}}).", { tz: localTZ }) : t.t("Times are interpreted as your local timezone.")}>
            <input ref={whenRef} type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
          </Field>
        )}
      </fieldset>

      <details className="evs-details" open={teamsOpen} onToggle={(e) => setTeamsOpen((e.target as HTMLDetailsElement).open)}>
        <summary>{t.t("Add teams now (optional)")}</summary>
        <p className="evs-muted evs-small">{t.t("Bulk-create teams up front. Skip this and add them by hand on the event page later if you prefer.")}</p>
        <div className="evs-grid2">
          <Field label={t.t("Number of teams")}>
            <input ref={teamCountRef} type="number" min={0} max={50} value={teamCount} onChange={(e) => setTeamCount(e.target.value)} />
          </Field>
          <Field label={t.t("Players per team")}>
            <input type="number" min={1} max={20} value={perTeam} onChange={(e) => setPerTeam(e.target.value)} />
          </Field>
          <Field label={t.t("Team name prefix")}>
            <input value={teamPrefix} onChange={(e) => setTeamPrefix(e.target.value)} />
          </Field>
          <Field label={t.t("Player name prefix")}>
            <input value={playerPrefix} onChange={(e) => setPlayerPrefix(e.target.value)} />
          </Field>
        </div>
        {teamN > 0 && (
          <p className={"evs-small" + (overCap ? " evs-bad" : " evs-muted")} aria-live="polite">
            {cap > 0
              ? t.t("{{total}} players of the {{cap}} the event pass covers.", { total: teamN * perN, cap })
              : t.t("{{total}} players in all.", { total: teamN * perN })}
          </p>
        )}
      </details>

      <label className="evs-check evs-attest">
        <input
          ref={attestRef}
          type="checkbox"
          checked={attest}
          aria-invalid={attestErr || undefined}
          aria-describedby={attestErr ? "evs-attest-err" : undefined}
          onChange={(e) => { setAttest(e.target.checked); if (e.target.checked) setAttestErr(false); }}
        />
        <span>{t.t("I confirm no participants under 13 will play this adventure, or I have parental consent for any who will.")}</span>
      </label>
      {attestErr && <p id="evs-attest-err" className="cs-field__error" role="alert">{t.t("Please confirm the player-age statement to continue.")}</p>}

      {error && <div ref={errorRef}><ErrorBox message={error} /></div>}
    </Dialog>
  );
}

function AdventureChoice({ adv, checked, pass, onPick }: { adv: AdventureRow; checked: boolean; pass: Assignment | undefined; onPick: () => void }) {
  const t = useT();
  const note = pass ? t.t("Event pass for {{n}} players", { n: pass.max_players }) : adv.shared ? t.t("Commissioned for you") : null;
  return (
    <label className={"evs-choice" + (checked ? " is-on" : "")}>
      <input type="radio" name="evs-adventure" checked={checked} onChange={onPick} />
      <AdventureCover title={adv.title} src={adv.avatar_url} size={32} />
      <span className="evs-stack">
        <strong className="evs-ellipsis">{adv.title}</strong>
        <span className="evs-muted evs-small">
          {adv.mine_to_edit ? t.t("yours") : CATALOG_ENABLED ? t.t("catalog") : t.t("shared")}
          {note && <> · {note}</>}
          {!adv.playable && <> · {t.t("not published yet")}</>}
        </span>
      </span>
    </label>
  );
}
