import { useEffect, useRef, useState } from "react";
import { ApiError, canAuthor } from "@zonventure/api";
import { contentT, useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { CATALOG_ENABLED, navigate } from "../../router.ts";
import { Button, Callout, Dialog, Empty, ErrorBox, Field, Loading, fmtMoney, useLoad, useToast, errorText } from "../../ui/index.ts";
import { SPOTS } from "@zonventure/ui";
import { createEvent, createTeam, listAdventures, listAssignments, pricing, type AdventureRow, type Assignment } from "./api.ts";
import { iconOrCover } from "../adventures/parts.tsx";
import { AdventureCover } from "./parts.tsx";
import "./events.css";

/**
 * Schedule an event: pick the adventure (unless given), name it, start
 * manually or at a time, optionally add teams. Nothing is paid up
 * front: each team is charged to the organisation as it starts. Opened
 * from the dashboard, the events list, an adventure's page and the
 * catalog. On success it navigates to the new event.
 *
 * Props are the contract other areas code against:
 *   slug          — preselect this adventure
 *   assignmentId  — use this event pass (one bought before pay per team)
 */
export interface NewEventProps {
  slug?: string;
  assignmentId?: number;
  onClose: () => void;
}

/** The adventures the dialog offers: with the catalog off, only what the Adventures page shows (the org's own, the commissioned, and those it holds a pass for). */
export function usableAdventures(all: AdventureRow[], assigns: Assignment[]): AdventureRow[] {
  return CATALOG_ENABLED ? all : all.filter((a) => a.mine_to_edit || a.shared || assigns.some((x) => x.adventure_slug === a.slug));
}

/** Whether an event can be started: some usable adventure is published. Until then a "new event" button only leads to a dead end. */
export function canStartEvent(all: AdventureRow[], assigns: Assignment[]): boolean {
  return usableAdventures(all, assigns).some((a) => a.playable);
}

/**
 * What the event is made from: an unused event pass from before pay per
 * team, or the adventure itself.
 */
type Ctx = { mode: "preset"; assignmentId: number } | { mode: "slug"; slug: string };

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

function NewEventForm({ slug, assignmentId, onClose }: NewEventProps) {
  const t = useT();
  const toast = useToast();
  const { state, refresh } = useSession();
  const user = state.status === "authenticated" ? state.user : null!;
  const loc = t.current();
  const price = pricing(user);
  const needsPicker = assignmentId === undefined && slug === undefined;

  const lists = useLoad(async (signal) => {
    const [all, assigns] = await Promise.all([listAdventures(signal), listAssignments(signal)]);
    const unused = assigns.filter((a) => !a.event_id);
    // With the catalog off, only what the Adventures page shows: the
    // org's own, the commissioned, and those it holds a pass for.
    const advs = usableAdventures(all, assigns);
    return { advs, unused };
  }, []);

  // Prices an operator changed since login show without a reload.
  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const [picked, setPicked] = useState<string | null>(slug ?? null);
  const [filter, setFilter] = useState("");
  const [name, setName] = useState("");
  const [later, setLater] = useState(false);
  const [when, setWhen] = useState("");
  const [teamsOpen, setTeamsOpen] = useState(false);
  const [teamCount, setTeamCount] = useState("0");
  const [perTeam, setPerTeam] = useState("3");
  // Null until typed in: the default follows the adventure's language.
  const [teamPrefixIn, setTeamPrefix] = useState<string | null>(null);
  const [playerPrefixIn, setPlayerPrefix] = useState<string | null>(null);
  const [attest, setAttest] = useState(false);
  const [attestErr, setAttestErr] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const whenRef = useRef<HTMLInputElement>(null);
  const attestRef = useRef<HTMLInputElement>(null);
  const attestErrRef = useRef<HTMLParagraphElement>(null);
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
    if (assignmentId !== undefined) return { mode: "preset", assignmentId };
    if (needsPicker) {
      if (!pickedAdv) return null;
      const pass = unused.find((x) => x.adventure_slug === pickedAdv.slug);
      if (pass) return { mode: "preset", assignmentId: pass.id };
      return { mode: "slug", slug: pickedAdv.slug };
    }
    return { mode: "slug", slug: slug! };
  })();

  const pass: Assignment | undefined = ctx?.mode === "preset" ? unused.find((x) => x.id === ctx.assignmentId) : undefined;
  const cap = pass?.max_players ?? 0;
  const teamN = teamsOpen ? Math.max(0, parseInt(teamCount, 10) || 0) : 0;
  const perN = Math.max(1, parseInt(perTeam, 10) || 0);
  const overCap = cap > 0 && teamN * perN > cap;

  const localTZ = (() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
    } catch {
      return "";
    }
  })();

  const adventureTitle = pickedAdv?.title ?? pass?.adventure_title ?? advs.find((a) => a.slug === (pass?.adventure_slug ?? slug))?.title ?? pass?.adventure_slug ?? slug ?? "";
  const chosenAdv = advs.find((a) => a.slug === (pickedAdv?.slug ?? pass?.adventure_slug ?? slug));
  const ct = contentT(chosenAdv?.language);
  const teamPrefix = teamPrefixIn ?? ct("Team");
  const playerPrefix = playerPrefixIn ?? ct("Player");

  async function submit() {
    if (busy || !ctx) return;
    setError(null);
    if (!attest) {
      setAttestErr(true);
      attestRef.current?.focus();
      // The statement sits at the foot of the form, under the footer on
      // a phone: bring its message into view, on every try, once drawn.
      requestAnimationFrame(() => attestErrRef.current?.scrollIntoView({ block: "nearest" }));
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
    const passId: number | undefined = ctx.mode === "preset" ? ctx.assignmentId : undefined;

    let id: number;
    try {
      const body = {
        name: name.trim(),
        attest_no_minors: true as const,
        ...(passId ? { assignment_id: passId } : ctx.mode === "slug" ? { adventure_slug: ctx.slug } : {}),
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
    const tp = teamPrefix.trim() || ct("Team");
    const pp = playerPrefix.trim() || ct("Player");
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
        <Empty title={t.t("No adventures you can use yet.")} illustration={SPOTS.noAdventures}>
          {author ? t.t("Create your own adventure to get started.") : t.t("An organization admin can create one for you.")}
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
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!ctx || overCap}>
            {t.t("Create event")}
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
              <AdventureChoice key={a.slug} adv={a} checked={a.slug === picked} pass={unused.find((x) => x.adventure_slug === a.slug)} onPick={() => setPicked(a.slug)} />
            ))}
            {shown.length === 0 && <p className="cs-note">{t.t("No adventures match.")}</p>}
          </div>
        </fieldset>
      ) : (
        adventureTitle && (
          <div className="evs-chosen">
            <AdventureCover title={adventureTitle} src={chosenAdv && iconOrCover(chosenAdv)} size={36} />
            <span className="cs-stack">
              <strong>{adventureTitle}</strong>
              {pass ? (
                <span className="cs-sub">{t.t("Event pass for {{n}} players", { n: pass.max_players })}</span>
              ) : needsPicker && pickedAdv && (pickedAdv.shared || !pickedAdv.playable) && (
                <span className="cs-sub">
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

      {!pass && (
        <p className="cs-note">
          {t.t("Nothing to pay now: each team costs {{price}} as it starts, its first {{n}} phones included, from your organisation's balance.", { price: fmtMoney(price.team, price.currency, loc), n: price.included })}{" "}
          <span>{price.vatExcluded ? t.t("excl. VAT") : t.t("incl. VAT")}</span>
        </p>
      )}

      <Field label={t.t("Name (optional)")}>
        <input value={name} maxLength={200} onChange={(e) => setName(e.target.value)} placeholder={t.t("e.g. “Spring outing”")} />
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
        <p className="cs-note">{t.t("Bulk-create teams up front. Skip this and add them by hand on the event page later if you prefer.")}</p>
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
          <p className={"cs-small cs-flush" + (overCap ? " evs-bad" : " cs-muted")} aria-live="polite">
            {cap > 0
              ? t.t("{{total}} players of the {{cap}} the event pass covers.", { total: teamN * perN, cap })
              : t.t("{{teams}} teams cost {{cost}} as they start.", { teams: teamN, cost: fmtMoney(price.cost(teamN, perN), price.currency, loc) })}
            {overCap && <> {t.t("Make fewer or smaller teams.")}</>}
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
      <details className="evs-attest-help">
        <summary>{t.t("What this means")}</summary>
        <p>{t.t("Players' phones send their location and answers, which is personal data. Children under 13 need a parent's or guardian's consent for that: a written yes to the activity and the game, such as the permission slip for the outing. You collect and keep it; we don't.")}</p>
        <a href="/manual#player-age" target="_blank" rel="noopener">{t.t("Read more about the player-age statement")}</a>
      </details>
      {attestErr && <p ref={attestErrRef} id="evs-attest-err" className="cs-field__error" role="alert">{t.t("Please confirm the player-age statement to continue.")}</p>}

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
      <AdventureCover title={adv.title} src={iconOrCover(adv)} size={32} />
      <span className="cs-stack">
        <strong className="cs-ellipsis">{adv.title}</strong>
        <span className="cs-sub">
          {adv.mine_to_edit ? t.t("yours") : CATALOG_ENABLED ? t.t("catalog") : t.t("shared")}
          {note && <> · {note}</>}
          {!adv.playable && <> · {t.t("not published yet")}</>}
        </span>
      </span>
    </label>
  );
}
