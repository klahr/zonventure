import { useState } from "react";
import { Copy, ExternalLink, Mail, Phone, Trash2 } from "lucide-react";
import { SPOTS } from "@zonventure/ui";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import {
  Button, Card, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Page, PageHeader, Pill, Tabs, errorText, fmtDate, fmtDateTime, fmtMoney, relTime, useDialogs, useLoad, useToast, type Tone,
} from "../../ui/index.ts";
import { deleteRequest, getQuoteSettings, listRequests, putQuoteSettings, updateRequest, type QuoteRequest, type QuoteSettings, type QuoteStatus } from "./api.ts";
import "./requests.css";

const STATES = ["new", "answered", "won", "lost", "all"] as const;
type Tab = (typeof STATES)[number];
const LABEL: Record<Tab, string> = { new: "New", answered: "Answered", won: "Won", lost: "Lost", all: "All" };
const TONE: Record<QuoteStatus, Tone> = { new: "warn", answered: "neutral", won: "good", lost: "bad" };

const OCCASION: Record<string, string> = { kickoff: "Kickoff", team_day: "Team day", conference: "Conference activity", customer_event: "Customer event", other: "Something else" };
const SCOPE: Record<string, string> = { ready_made: "A ready-made adventure they run themselves", adapted: "An adventure adapted for them", new: "A new adventure made for them" };
const TIME: Record<string, string> = { morning: "Morning", afternoon: "Afternoon", evening: "Evening" };
const DURATION: Record<string, string> = { "1h": "1 hour", "2h": "2 hours", "3h": "3 hours", half_day: "Half a day" };
/** What an organisation can leave out of its form, in groups, by key. */
const OFFERS: { title: string; items: { id: string; label: string }[] }[] = [
  { title: "Adventures", items: [{ id: "ready_made", label: "Ready-made, run by them" }, { id: "adapted", label: "Adapted for them" }, { id: "new", label: "Made new for them" }] },
  { title: "On the day", items: [{ id: "game_master", label: "A game master" }, { id: "prize_ceremony", label: "A prize ceremony" }, { id: "big_screen", label: "Results on a big screen" }] },
  { title: "Adapting it", items: [{ id: "own_questions", label: "Their own questions" }, { id: "logo", label: "Their logo" }, { id: "accessibility", label: "Accessible for everyone" }] },
  { title: "Languages", items: [{ id: "sv", label: "Swedish" }, { id: "en", label: "English" }] },
];
const KINDS = ["ready_made", "adapted", "new"];

const EXTRA: Record<string, string> = { own_questions: "Their own questions", logo: "Their logo", accessibility: "Accessible for everyone", prize_ceremony: "A prize ceremony", big_screen: "Results on a big screen" };

/**
 * Requests for a quote from the organisation's public form: a company
 * planning a kickoff or a team day says what it wants, and the
 * organisation answers it with its own quote, by email. Here it keeps
 * track of them (New, Answered, Won, Lost) and turns the form on or off.
 * Org admins only: requests hold customers' contact details.
 */
export function RequestsPage({ tab }: { tab?: string | undefined }) {
  const t = useT();
  const st: Tab = (STATES as readonly string[]).includes(tab ?? "") ? (tab as Tab) : "new";
  const list = useLoad((s) => listRequests(st, s), [st]);
  const settings = useLoad((s) => getQuoteSettings(s), []);
  const counts = list.data?.counts ?? {};
  const total = (["new", "answered", "won", "lost"] as const).reduce((a, s) => a + (counts[s] ?? 0), 0);
  const rows = list.data?.requests ?? [];

  return (
    <Page>
      <PageHeader
        title={
          <>
            {t.t("Requests")}
            <HelpPopover
              label={t.t("About requests")}
              body={t.t("Companies planning a kickoff, a team day or another event ask you for a quote on your public page. Each request is emailed to you, and kept here for a year after its last change. Answer by email with your quote, then mark how it went.")}
            />
          </>
        }
        subtitle={(counts.new ?? 0) > 0
          ? t.plural(counts.new ?? 0, { one: t.t("{{n}} new request"), other: t.t("{{n}} new requests") })
          : t.t("Requests for a quote from your public page.")}
      />
      {settings.error && !settings.data ? (
        <ErrorBox message={settings.error} retry={settings.reload} retryLabel={t.t("Retry")} />
      ) : settings.data ? (
        <SettingsCard settings={settings.data} onSaved={settings.set} />
      ) : null}
      {(total > 0 || st !== "new") && (
        <Tabs
          label={t.t("Requests")}
          current={st}
          tabs={STATES.map((s) => {
            const n = s === "all" ? 0 : counts[s] ?? 0;
            return { id: s, href: href({ name: "requests", tab: s }), label: <>{t.t(LABEL[s])}{n > 0 && <span className="rq-count">{n}</span>}</> };
          })}
        />
      )}
      {list.error && !list.data ? (
        <ErrorBox message={list.error} retry={list.reload} retryLabel={t.t("Retry")} />
      ) : !list.data ? (
        <Loading label={t.t("Loading…")} />
      ) : rows.length === 0 ? (
        <Empty
          title={total === 0 ? t.t("No requests yet.") : st === "new" ? t.t("No new requests.") : t.t("No requests here.")}
          {...(total === 0 ? { illustration: SPOTS.noRedemptions } : {})}
        />
      ) : (
        <ul className="rq-list">
          {rows.map((q) => <RequestItem key={q.id} q={q} showState={st === "all"} reload={list.reload} />)}
        </ul>
      )}
    </Page>
  );
}

/** Whether the form is on, its intro, and where it is. */
function SettingsCard({ settings, onSaved }: { settings: QuoteSettings; onSaved: (s: QuoteSettings) => void }) {
  const t = useT();
  const toast = useToast();
  const [open, setOpen] = useState(settings.open);
  const [intro, setIntro] = useState(settings.intro);
  const [off, setOff] = useState<string[]>(settings.off);
  const [busy, setBusy] = useState(false);
  const sameOff = off.length === settings.off.length && off.every((k) => settings.off.includes(k));
  const changed = open !== settings.open || intro.trim() !== settings.intro || !sameOff;
  // At least one kind of adventure stays on.
  const lastKind = (id: string) => KINDS.includes(id) && KINDS.filter((k) => !off.includes(k)).length === 1 && !off.includes(id);
  const save = async () => {
    setBusy(true);
    try {
      const s = await putQuoteSettings({ open, intro, off });
      onSaved(s);
      setIntro(s.intro);
      setOff(s.off);
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(settings.url);
      toast.ok(t.t("Link copied"));
    } catch {
      toast.err(t.t("Couldn't copy the link."));
    }
  };
  return (
    <Card
      title={t.t("Your request form")}
      subtitle={t.t("A guided form on your public page where companies ask you for a quote for a live event.")}
      actions={settings.open && (
        <Button size="sm" variant="ghost" icon={<ExternalLink size={ICON.sm} aria-hidden />} onClick={() => window.open(settings.url, "_blank", "noopener")}>
          {t.t("Open the form")}
        </Button>
      )}
    >
      <div className="rq-settings">
        <label className="rq-check">
          <input type="checkbox" checked={open} onChange={(e) => setOpen(e.target.checked)} />
          <span>{t.t("Take requests: show “Ask for a quote” on your public page")}</span>
        </label>
        <Field label={t.t("Intro")} hint={t.t("A few lines at the top of the form: who you are, where you arrange events, prices from. {{n}} characters at most.", { n: 600 })}>
          <textarea rows={3} maxLength={600} value={intro} onChange={(e) => setIntro(e.target.value)} />
        </Field>
        <fieldset className="rq-offers">
          <legend>{t.t("What you offer")}</legend>
          <p className="rq-muted">{t.t("Untick what you can't do: the form doesn't ask for it.")}</p>
          <div className="rq-offers__groups">
            {OFFERS.map((g) => (
              <div key={g.title} className="rq-offers__group">
                <span className="rq-label">{t.t(g.title)}</span>
                {g.items.map((o) => (
                  <label key={o.id} className="rq-check rq-check--plain" title={lastKind(o.id) ? t.t("Offer at least one kind of adventure.") : undefined}>
                    <input
                      type="checkbox"
                      checked={!off.includes(o.id)}
                      disabled={lastKind(o.id)}
                      onChange={(e) => setOff(e.target.checked ? off.filter((k) => k !== o.id) : [...off, o.id])}
                    />
                    <span>{t.t(o.label)}</span>
                  </label>
                ))}
              </div>
            ))}
          </div>
        </fieldset>
        {settings.open && (
          <div className="rq-url">
            <input readOnly value={settings.url} aria-label={t.t("The form's address")} onFocus={(e) => e.currentTarget.select()} />
            <Button size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={copy}>{t.t("Copy link")}</Button>
          </div>
        )}
        <p className="rq-muted">
          {settings.contact
            ? t.t("Requests are emailed to {{email}}, your organisation's contact address.", { email: settings.contact })
            : t.t("Requests are emailed to your organisation's contact address.")}{" "}
          <a href={href({ name: "settings", tab: "organization" })}>{t.t("Change it")}</a>
        </p>
        <div>
          <Button variant="primary" disabled={!changed || busy} onClick={save}>{busy ? t.t("Saving…") : t.t("Save")}</Button>
        </div>
      </div>
    </Card>
  );
}

function RequestItem({ q, showState, reload }: { q: QuoteRequest; showState: boolean; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const locale = t.current();
  const a = q.answers;
  const [notes, setNotes] = useState(q.notes);
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await fn();
      toast.ok(done);
      reload();
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const setStatus = (status: QuoteStatus) => run(() => updateRequest(q.id, { status }), t.t("Marked {{status}}", { status: t.t(LABEL[status]).toLowerCase() }));
  const remove = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Delete this request?"),
      message: t.t("{{name}}'s request and your notes go for good. Do this when they ask, or when it is junk.", { name: q.name }),
      confirmLabel: t.t("Delete"),
      cancelLabel: t.t("Back"),
      danger: true,
    });
    if (ok) await run(() => deleteRequest(q.id), t.t("Request deleted"));
  };

  const when = [
    a.date ? fmtDate(a.date, locale) : a.month ? t.t("Sometime in {{month}}", { month: monthName(a.month, locale) }) : "",
    a.time_of_day && TIME[a.time_of_day] ? t.t(TIME[a.time_of_day]!) : "",
    a.duration && DURATION[a.duration] ? t.t(DURATION[a.duration]!) : "",
  ].filter(Boolean).join(", ");
  const extras = [
    ...(a.extras ?? []).map((x) => (EXTRA[x] ? t.t(EXTRA[x]!) : x)),
    ...(a.languages ?? []).map((l) => (l === "sv" ? t.t("in Swedish") : l === "en" ? t.t("in English") : l)),
  ].join(", ");
  const subject = t.t("Your request: {{occasion}}", { occasion: t.t(OCCASION[a.occasion] ?? "Something else") });

  return (
    <li className="rq-item">
      <div className="rq-item__main">
        <div className="rq-item__col">
          <span className="rq-label">{t.t("Occasion")}</span>
          <strong>{t.t(OCCASION[a.occasion] ?? "Something else")}</strong>
          <span className="rq-sub" title={fmtDateTime(q.created_at, locale)}>{t.t("Received {{when}}", { when: relTime(q.created_at, locale) })}</span>
          {showState && <Pill tone={TONE[q.status]}>{t.t(LABEL[q.status])}</Pill>}
        </div>
        <div className="rq-item__col">
          <span className="rq-label">{t.t("When")}</span>
          <span>{when}</span>
        </div>
        <div className="rq-item__col">
          <span className="rq-label">{t.t("Size and place")}</span>
          <span>{t.plural(a.people, { one: t.t("{{n}} person"), other: t.t("{{n}} people") })}</span>
          <span className="rq-sub">{[a.city, a.venue].filter(Boolean).join(", ")}{a.help_choose ? ` · ${t.t("wants help choosing where")}` : ""}</span>
        </div>
        <div className="rq-item__col">
          <span className="rq-label">{t.t("Customer")}</span>
          <strong>{q.name}</strong>
          {q.company && <span>{q.company}</span>}
          <a className="rq-sub" href={`mailto:${q.email}`}>{q.email}</a>
          {q.phone && <a className="rq-sub" href={`tel:${q.phone.replace(/[^\d+]/g, "")}`}>{q.phone}</a>}
        </div>
      </div>
      <dl className="rq-details">
        <dt>{t.t("Wants")}</dt>
        <dd>{t.t(SCOPE[a.scope] ?? a.scope)}{a.game_master ? `, ${t.t("with a game master on the day")}` : ""}</dd>
        {extras && (
          <>
            <dt>{t.t("Also")}</dt>
            <dd>{extras}</dd>
          </>
        )}
        {a.budget && (
          <>
            <dt>{t.t("Budget")}</dt>
            <dd>{budgetLabel(a.budget, locale, t.t)}</dd>
          </>
        )}
        {a.message && (
          <>
            <dt>{t.t("Message")}</dt>
            <dd className="rq-message">{a.message}</dd>
          </>
        )}
      </dl>
      <div className="rq-notes">
        <Field label={t.t("Your notes")} hint={t.t("Only your organisation sees them.")}>
          <textarea rows={2} maxLength={2000} value={notes} onChange={(e) => setNotes(e.target.value)} />
        </Field>
        {notes !== q.notes && (
          <Button size="sm" disabled={busy} onClick={() => run(() => updateRequest(q.id, { notes }), t.t("Notes saved"))}>{t.t("Save notes")}</Button>
        )}
      </div>
      <div className="rq-item__actions">
        <Button size="sm" variant="primary" icon={<Mail size={ICON.sm} aria-hidden />}
          onClick={() => { window.location.href = `mailto:${q.email}?subject=${encodeURIComponent(subject)}`; }}>
          {t.t("Reply by email")}
        </Button>
        {q.phone && (
          <Button size="sm" icon={<Phone size={ICON.sm} aria-hidden />} onClick={() => { window.location.href = `tel:${q.phone.replace(/[^\d+]/g, "")}`; }}>
            {t.t("Call")}
          </Button>
        )}
        <span className="rq-spacer" />
        {q.status === "new" && (
          <Button size="sm" disabled={busy} onClick={() => setStatus("answered")}>{t.t("Mark answered")}</Button>
        )}
        {q.status !== "won" && <Button size="sm" disabled={busy} onClick={() => setStatus("won")}>{t.t("Won")}</Button>}
        {q.status !== "lost" && <Button size="sm" disabled={busy} onClick={() => setStatus("lost")}>{t.t("Lost")}</Button>}
        {q.status !== "new" && <Button size="sm" variant="ghost" disabled={busy} onClick={() => setStatus("new")}>{t.t("Back to new")}</Button>}
        <Button size="icon" variant="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={t.t("Delete")} title={t.t("Delete")} disabled={busy} onClick={remove} />
      </div>
      <p className="rq-sub rq-item__foot" title={fmtDateTime(q.updated_at, locale)}>
        {t.t("Deleted {{when}} unless it changes again.", { when: fmtDate(new Date(new Date(q.updated_at).getTime() + 365 * 86_400_000).toISOString(), locale) })}
      </p>
    </li>
  );
}

function monthName(ym: string, locale: string): string {
  const [y, m] = ym.split("-").map(Number);
  return new Date(y!, m! - 1, 1).toLocaleDateString(locale, { month: "long", year: "numeric" });
}

function budgetLabel(id: string, locale: string, tt: (s: string, v?: Record<string, string | number>) => string): string {
  const k = (n: number) => fmtMoney(n * 100, undefined, locale);
  switch (id) {
    case "under_5k":
      return tt("Under {{amount}}", { amount: k(5000) });
    case "5k_15k":
      return `${k(5000)} – ${k(15000)}`;
    case "15k_40k":
      return `${k(15000)} – ${k(40000)}`;
    case "over_40k":
      return tt("Over {{amount}}", { amount: k(40000) });
    default:
      return tt("Doesn't know yet");
  }
}
