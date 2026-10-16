import { useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError } from "@zonventure/api";
import { Icon, themeStyle } from "@zonventure/trail";
import { SPOTS } from "@zonventure/ui";
import { getQuoteForm, money, sendQuoteRequest, type QuoteAnswers, type QuoteForm } from "./api.ts";
import { GuideShell } from "./Guide.tsx";
import { useT } from "./i18n.tsx";
import { leave } from "./embed.ts";
import { useLook } from "./look.ts";
import { Spot } from "./pictures.tsx";

/**
 * Asking an organisation for a quote: a company planning a live event
 * answers one question a screen, then sends it. The organisation gets it
 * by email and in its console, and answers with its own quote. The
 * answers are kept in the tab (sessionStorage), so a reload loses
 * nothing; the screen is mirrored in history.state, so the phone's back
 * button steps back through them.
 */

const STEPS = ["welcome", "occasion", "when", "people", "where", "scope", "extras", "budget", "contact", "summary"] as const;
type Step = (typeof STEPS)[number] | "done";

interface Draft {
  answers: QuoteAnswers;
  /** The date is open: a month instead. */
  flexible: boolean;
  name: string;
  company: string;
  email: string;
  phone: string;
}

const OCCASIONS = [
  { id: "kickoff", icon: "briefcase", label: "Kickoff", hint: "The whole company, or a department, at the start of a year or a season." },
  { id: "team_day", icon: "users", label: "Team day", hint: "A team getting out of the office together." },
  { id: "conference", icon: "calendar", label: "Conference activity", hint: "Something to do between the talks, or the evening before." },
  { id: "customer_event", icon: "sparkle", label: "Customer event", hint: "Customers or partners, played together with you." },
  { id: "other", icon: "compass", label: "Something else", hint: "You can say more in a later step." },
] as const;

const SCOPES = [
  { id: "ready_made", icon: "cards", label: "A ready-made adventure", hint: "You run it yourselves, on your own phones. The quickest and the cheapest." },
  { id: "adapted", icon: "palette", label: "One adapted for you", hint: "A ready adventure with your logo, starting or ending at your venue, with some of your own questions." },
  { id: "new", icon: "sparkle", label: "A new one made for you", hint: "Made from scratch for your place and your theme." },
] as const;

const TIMES = [
  { id: "morning", label: "Morning" },
  { id: "afternoon", label: "Afternoon" },
  { id: "evening", label: "Evening" },
] as const;

const DURATIONS = [
  { id: "1h", label: "1 hour" },
  { id: "2h", label: "2 hours" },
  { id: "3h", label: "3 hours" },
  { id: "half_day", label: "Half a day" },
] as const;

const EXTRAS = [
  { id: "own_questions", icon: "quiz", label: "Our own questions", hint: "About the company, the team or the year." },
  { id: "logo", icon: "palette", label: "Our logo", hint: "On every phone, through the game." },
  { id: "accessibility", icon: "users", label: "Accessible for everyone", hint: "Short walks, no stairs, room for a wheelchair." },
  { id: "prize_ceremony", icon: "sparkle", label: "A prize ceremony", hint: "The results revealed together at the end." },
  { id: "big_screen", icon: "phone", label: "Results on a big screen", hint: "Teams' progress shown live, at the venue." },
] as const;

const BUDGETS = ["under_5k", "5k_15k", "15k_40k", "over_40k", "unknown"] as const;

const storageKey = (org: string) => `zv-request-${org}`;

function today(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** The next 18 months, as YYYY-MM, starting with this one. */
function months(): string[] {
  const d = new Date();
  return Array.from({ length: 18 }, (_, i) => {
    const m = new Date(d.getFullYear(), d.getMonth() + i, 1);
    return `${m.getFullYear()}-${String(m.getMonth() + 1).padStart(2, "0")}`;
  });
}

function monthName(ym: string, locale: string): string {
  const [y, m] = ym.split("-").map(Number);
  return new Date(y!, m! - 1, 1).toLocaleDateString(locale, { month: "long", year: "numeric" });
}

function dateName(ymd: string, locale: string): string {
  const [y, m, d] = ymd.split("-").map(Number);
  return new Date(y!, m! - 1, d!).toLocaleDateString(locale, { weekday: "long", day: "numeric", month: "long", year: "numeric" });
}

function blankDraft(lang: string): Draft {
  return {
    answers: { occasion: "", date: "", month: "", time_of_day: "afternoon", duration: "2h", people: 50, city: "", venue: "", help_choose: false, scope: "adapted", game_master: false, extras: [], languages: [lang], budget: "", message: "" },
    flexible: false,
    name: "",
    company: "",
    email: "",
    phone: "",
  };
}

function loadDraft(org: string, lang: string): Draft {
  try {
    const saved = sessionStorage.getItem(storageKey(org));
    if (saved) return { ...blankDraft(lang), ...(JSON.parse(saved) as Draft) };
  } catch {
    // A private window, or a broken entry: start over.
  }
  return blankDraft(lang);
}

/** What the organisation offers through its form: the rest is left out. */
function offers(form: QuoteForm | null) {
  const off = new Set(form?.off ?? []);
  const scopes = SCOPES.filter((o) => !off.has(o.id));
  return {
    scopes,
    gameMaster: !off.has("game_master"),
    extras: EXTRAS.filter((o) => !off.has(o.id)),
    languages: (["sv", "en"] as const).filter((l) => !off.has(l)),
    off,
    // The scope question has something to ask: a choice of kinds, or the game master.
    askScope: scopes.length > 1 || !off.has("game_master"),
  };
}

const validEmail = (s: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s.trim());

export function Request({ org }: { org: string }) {
  const i18n = useT();
  const t = i18n.t;
  const locale = i18n.current();
  const [form, setForm] = useState<QuoteForm | null>(null);
  const [missing, setMissing] = useState(false);
  const [draft, setDraft] = useState<Draft>(() => loadDraft(org, locale.startsWith("sv") ? "sv" : "en"));
  const [step, setStep] = useState<Step>(() => ((history.state as { request?: Step } | null)?.request ?? "welcome"));
  // Where Summary sent them to change an answer: Next goes back there.
  const [editing, setEditing] = useState(false);
  const [website, setWebsite] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const top = useRef<HTMLDivElement>(null);
  // The organisation's look on the whole page, as on its public page.
  useLook(form?.look);

  useEffect(() => {
    getQuoteForm(org).then(setForm, () => setMissing(true));
  }, [org]);
  // Answers for what the organisation doesn't offer go: a draft kept from
  // before it changed them, or the defaults.
  useEffect(() => {
    if (!form) return;
    const o = offers(form);
    const ids = o.scopes.map((s) => s.id as string);
    setDraft((d) => {
      const a = d.answers;
      return {
        ...d,
        answers: {
          ...a,
          scope: ids.includes(a.scope) ? a.scope : ids.length === 1 ? ids[0]! : "",
          game_master: o.gameMaster && !!a.game_master,
          extras: (a.extras ?? []).filter((x) => !o.off.has(x)),
          languages: o.languages.length === 1 ? [...o.languages] : (a.languages ?? []).filter((l) => !o.off.has(l)),
        },
      };
    });
  }, [form]);
  const offered = offers(form);
  // The screens there are: the scope question only when it asks something.
  const steps = STEPS.filter((s) => s !== "scope" || offered.askScope);
  useEffect(() => {
    try {
      sessionStorage.setItem(storageKey(org), JSON.stringify(draft));
    } catch {
      // Not kept: a reload starts over.
    }
  }, [org, draft]);
  useEffect(() => {
    history.replaceState({ request: step }, "");
    const pop = (e: PopStateEvent) => {
      const s = (e.state as { request?: Step } | null)?.request;
      if (s) {
        setStep(s);
        setEditing(false);
      }
    };
    addEventListener("popstate", pop);
    return () => removeEventListener("popstate", pop);
    // The first screen only; later ones are pushed by go.
  }, []);

  const go = (next: Step) => {
    history.pushState({ request: next }, "");
    setStep(next);
    setError(null);
    top.current?.closest(".trail-guide")?.scrollTo(0, 0);
  };
  const next = () => {
    if (editing) {
      setEditing(false);
      go("summary");
      return;
    }
    const i = steps.indexOf(step as (typeof STEPS)[number]);
    go(steps[i + 1] ?? "summary");
  };
  const edit = (s: Step) => {
    setEditing(true);
    go(s);
  };
  const a = draft.answers;
  const set = (patch: Partial<QuoteAnswers>) => setDraft((d) => ({ ...d, answers: { ...d.answers, ...patch } }));
  const toggle = (list: string[] | undefined, id: string) => ((list ?? []).includes(id) ? (list ?? []).filter((x) => x !== id) : [...(list ?? []), id]);

  if (missing) {
    return (
      <section className="party-card">
        <Spot src={SPOTS.notFound} />
        <h1>{t("This form can't be found")}</h1>
        <p>{t("The organisation may not take requests here any more.")}</p>
      </section>
    );
  }
  if (!form) return <p className="zv-muted">{t("Loading…")}</p>;

  const close = () => leave(`/o/${encodeURIComponent(org)}`);
  const at = steps.indexOf(step as (typeof STEPS)[number]);
  const progress = step === "welcome" || step === "done" ? undefined : at / (steps.length - 1);
  const brand = (
    <span className="req-brand">
      {form.logo_url ? <img src={form.logo_url} alt="" /> : <span className="req-brand__initial" aria-hidden="true">{form.org_name.slice(0, 1)}</span>}
      <span>{form.org_name}</span>
    </span>
  );
  // The welcome and the thanks open on the organisation's cover, with its logo.
  const hero = (
    <div className="req-hero" style={form.cover_url ? { backgroundImage: `url(${form.cover_url})` } : undefined}>
      {form.logo_url && <img className="req-hero__logo" src={form.logo_url} alt={form.org_name} />}
    </div>
  );
  const shell = (body: ReactNode, foot: ReactNode, opts: { hero?: boolean } = {}) => (
    <GuideShell
      style={themeStyle(form.look)}
      back={step !== "welcome" && step !== "done" ? () => history.back() : undefined}
      close={step === "done" ? undefined : close}
      status={brand}
      progress={progress}
      foot={foot}
      hero={opts.hero ? hero : undefined}
    >
      <div ref={top} />
      {body}
    </GuideShell>
  );
  const nextBtn = (label: string, disabled?: boolean) => (
    <button type="button" className="trail-btn trail-btn--primary trail-guide__next" disabled={disabled} onClick={next}>
      {label}
      <Icon name="right" size={18} />
    </button>
  );
  // Steps 1–8: the questions, between the welcome and the summary.
  const count = steps.length - 2;
  const heading = (title: string, lead?: string) => (
    <>
      {at > 0 && at <= count && <p className="req-step">{t("Question {{n}} of {{total}}", { n: at, total: count })}</p>}
      <h1 className="trail-guide__title">{title}</h1>
      {lead && <p className="trail-guide__lead">{lead}</p>}
    </>
  );

  if (step === "done") {
    return shell(
      <div className="req-done">
        <span className="req-done__mark" aria-hidden="true">
          <Icon name="check" size={40} />
        </span>
        {heading(t("Thanks — your request is on its way"), t("{{org}} will get back to you with a quote. We've emailed you a copy of what you asked for.", { org: form.org_name }))}
      </div>,
      <button type="button" className="trail-btn trail-btn--primary trail-guide__next" onClick={close}>
        {t("Back to {{org}}", { org: form.org_name })}
      </button>,
      { hero: true },
    );
  }

  if (step === "welcome") {
    return shell(
      <>
        {heading(t("Ask {{org}} for a quote", { org: form.org_name }), t("Planning a kickoff, a team day or another event for your company? Answer a few questions — it takes about two minutes."))}
        {form.intro && <blockquote className="req-intro">{form.intro}</blockquote>}
        <ol className="req-how">
          <li>
            <span className="req-how__icon" aria-hidden="true"><Icon name="pencil" size={20} /></span>
            <span><strong>{t("You answer")}</strong><small>{t("One question at a time. Go back and change anything before you send.")}</small></span>
          </li>
          <li>
            <span className="req-how__icon" aria-hidden="true"><Icon name="mail" size={20} /></span>
            <span><strong>{t("{{org}} sends a quote", { org: form.org_name })}</strong><small>{t("By email, with what they suggest and what it costs.")}</small></span>
          </li>
          <li>
            <span className="req-how__icon" aria-hidden="true"><Icon name="check" size={20} /></span>
            <span><strong>{t("You decide")}</strong><small>{t("Nothing is booked or paid: {{org}} answers with a quote.", { org: form.org_name })}</small></span>
          </li>
        </ol>
      </>,
      nextBtn(t("Start")),
      { hero: true },
    );
  }

  if (step === "occasion") {
    return shell(
      <>
        {heading(t("What's the occasion?"), t("It helps {{org}} suggest the right kind of adventure.", { org: form.org_name }))}
        <Choices
          options={OCCASIONS.map((o) => ({ id: o.id, icon: o.icon, label: t(o.label), hint: t(o.hint) }))}
          value={a.occasion}
          onChange={(occasion) => set({ occasion })}
        />
      </>,
      nextBtn(t("Next"), !a.occasion),
    );
  }

  if (step === "when") {
    const ok = draft.flexible ? !!a.month : !!a.date;
    return shell(
      <>
        {heading(t("When?"), t("A rough idea is enough. Popular dates go early, so say if it's set."))}
        <div className="req-switch" role="radiogroup" aria-label={t("Date")}>
          <button type="button" role="radio" aria-checked={!draft.flexible} className={!draft.flexible ? "is-on" : ""} onClick={() => setDraft((d) => ({ ...d, flexible: false }))}>
            {t("A set date")}
          </button>
          <button type="button" role="radio" aria-checked={draft.flexible} className={draft.flexible ? "is-on" : ""} onClick={() => setDraft((d) => ({ ...d, flexible: true }))}>
            {t("A month, flexible")}
          </button>
        </div>
        {draft.flexible ? (
          <label className="req-field">
            <span>{t("Which month?")}</span>
            <select value={a.month ?? ""} onChange={(e) => set({ month: e.target.value })}>
              <option value="">{t("Choose a month")}</option>
              {months().map((m) => (
                <option key={m} value={m}>
                  {monthName(m, locale)}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <label className="req-field">
            <span>{t("Which day?")}</span>
            <input type="date" min={today()} value={a.date ?? ""} onChange={(e) => set({ date: e.target.value })} />
          </label>
        )}
        <fieldset className="req-group">
          <legend>{t("Time of day")}</legend>
          <Chips options={TIMES.map((o) => ({ id: o.id, label: t(o.label) }))} value={a.time_of_day ?? ""} onChange={(time_of_day) => set({ time_of_day })} />
        </fieldset>
        <fieldset className="req-group">
          <legend>{t("How long?")}</legend>
          <Chips options={DURATIONS.map((o) => ({ id: o.id, label: t(o.label) }))} value={a.duration ?? ""} onChange={(duration) => set({ duration })} />
        </fieldset>
      </>,
      nextBtn(t("Next"), !ok),
    );
  }

  if (step === "people") {
    const n = a.people;
    const teams = Math.max(1, Math.round(n / 5));
    const step5 = (d: number) => set({ people: Math.min(10000, Math.max(1, n + d)) });
    return shell(
      <>
        {heading(t("How many people?"), t("Everyone plays in teams of 4–5."))}
        <div className="req-people">
          <button type="button" onClick={() => step5(-5)} disabled={n <= 1} aria-label={t("Fewer")}>
            −
          </button>
          <input
            type="number"
            inputMode="numeric"
            min={1}
            max={10000}
            value={n || ""}
            aria-label={t("Number of people")}
            onChange={(e) => set({ people: Math.min(10000, Math.max(0, Number.parseInt(e.target.value, 10) || 0)) })}
          />
          <button type="button" onClick={() => step5(5)} disabled={n >= 10000} aria-label={t("More")}>
            +
          </button>
        </div>
        {n > 0 && <p className="req-note">{i18n.plural(teams, { one: t("≈ {{n}} team of 4–5"), other: t("≈ {{n}} teams of 4–5") })}</p>}
      </>,
      nextBtn(t("Next"), n < 1),
    );
  }

  if (step === "where") {
    return shell(
      <>
        {heading(t("Where?"), t("The town, and if you know it, the venue where it should start or end — your office, a hotel, the restaurant for dinner after."))}
        <label className="req-field">
          <span>{t("Town or area")}</span>
          <input value={a.city} maxLength={200} autoComplete="address-level2" placeholder={t("Stockholm")} onChange={(e) => set({ city: e.target.value })} />
        </label>
        <label className="req-field">
          <span>{t("Venue or address (optional)")}</span>
          <input value={a.venue ?? ""} maxLength={200} placeholder={t("Our office, or a hotel")} onChange={(e) => set({ venue: e.target.value })} />
        </label>
        <label className="req-check">
          <input type="checkbox" checked={!!a.help_choose} onChange={(e) => set({ help_choose: e.target.checked })} />
          <span>{t("We'd like help choosing where")}</span>
        </label>
      </>,
      nextBtn(t("Next"), !a.city.trim()),
    );
  }

  if (step === "scope") {
    return shell(
      <>
        {heading(t("How much should {{org}} do?", { org: form.org_name }), t("Pick what sounds closest. You can talk it through when they answer."))}
        {offered.scopes.length > 1 && (
          <Choices options={offered.scopes.map((o) => ({ id: o.id, icon: o.icon, label: t(o.label), hint: t(o.hint) }))} value={a.scope} onChange={(scope) => set({ scope })} />
        )}
        {offered.gameMaster && (
          <label className="req-check req-check--card">
            <input type="checkbox" checked={!!a.game_master} onChange={(e) => set({ game_master: e.target.checked })} />
            <span className="req-tile__icon" aria-hidden="true">
              <Icon name="users" size={20} />
            </span>
            <span>
              <strong>{t("A game master on the day")}</strong>
              <small>{t("Someone from {{org}} starts the game, answers questions and hands out the prizes.", { org: form.org_name })}</small>
            </span>
          </label>
        )}
      </>,
      nextBtn(t("Next"), !a.scope),
    );
  }

  if (step === "extras") {
    return shell(
      <>
        {heading(t("Anything else?"), offered.extras.length > 0 ? t("Tick what you'd like. All of it is optional.") : undefined)}
        <ul className="req-ticks">
          {offered.extras.map((o) => (
            <li key={o.id}>
              <label className="req-check req-check--card">
                <input type="checkbox" checked={(a.extras ?? []).includes(o.id)} onChange={() => set({ extras: toggle(a.extras, o.id) })} />
                <span className="req-tile__icon" aria-hidden="true"><Icon name={o.icon} size={20} /></span>
                <span>
                  <strong>{t(o.label)}</strong>
                  <small>{t(o.hint)}</small>
                </span>
              </label>
            </li>
          ))}
        </ul>
        {offered.languages.length > 1 && (
          <fieldset className="req-group">
            <legend>{t("In which language?")}</legend>
            <div className="req-chips">
              {(["sv", "en"] as const).map((l) => (
                <button key={l} type="button" aria-pressed={(a.languages ?? []).includes(l)} className={(a.languages ?? []).includes(l) ? "is-on" : ""} onClick={() => set({ languages: toggle(a.languages, l) })}>
                  {l === "sv" ? t("Swedish") : t("English")}
                </button>
              ))}
            </div>
          </fieldset>
        )}
        <label className="req-field">
          <span>{t("Something to tell {{org}}? (optional)", { org: form.org_name })}</span>
          <textarea rows={4} maxLength={2000} value={a.message ?? ""} placeholder={t("A theme you'd like, who's coming, what you did last year…")} onChange={(e) => set({ message: e.target.value })} />
        </label>
      </>,
      nextBtn(t("Next")),
    );
  }

  if (step === "budget") {
    return shell(
      <>
        {heading(t("Is there a budget?"), t("Optional. It helps {{org}} suggest what fits, rather than the most or the least.", { org: form.org_name }))}
        <Choices options={BUDGETS.map((b) => ({ id: b, label: budgetLabel(b, form.currency, locale, t) }))} value={a.budget ?? ""} onChange={(budget) => set({ budget: budget === a.budget ? "" : budget })} />
      </>,
      nextBtn(a.budget ? t("Next") : t("Skip")),
    );
  }

  if (step === "contact") {
    const ok = draft.name.trim() && draft.company.trim() && validEmail(draft.email);
    const field = (key: "name" | "company" | "email" | "phone", label: string, extra: Partial<React.InputHTMLAttributes<HTMLInputElement>>) => (
      <label className="req-field">
        <span>{label}</span>
        <input value={draft[key]} maxLength={key === "phone" ? 40 : 200} onChange={(e) => setDraft((d) => ({ ...d, [key]: e.target.value }))} {...extra} />
      </label>
    );
    return shell(
      <>
        {heading(t("Who should {{org}} answer?", { org: form.org_name }))}
        {field("name", t("Your name"), { autoComplete: "name" })}
        {field("company", t("Company"), { autoComplete: "organization" })}
        {field("email", t("Email"), { type: "email", autoComplete: "email", inputMode: "email" })}
        {field("phone", t("Phone (optional)"), { type: "tel", autoComplete: "tel", inputMode: "tel" })}
        {/* The spam trap: hidden from people, filled in by bots. */}
        <div className="req-trap" aria-hidden="true">
          <label>
            Website
            <input name="website" tabIndex={-1} autoComplete="off" value={website} onChange={(e) => setWebsite(e.target.value)} />
          </label>
        </div>
        <p className="req-note">
          {t("{{org}} gets your answers and contact details to answer you, and keeps them for at most a year.", { org: form.org_name })}{" "}
          <a href="/privacy" target="_blank" rel="noopener">
            {t("Privacy")}
          </a>
        </p>
      </>,
      nextBtn(t("Next"), !ok),
    );
  }

  // Summary: every answer, each one tappable to change, then Send.
  const send = async () => {
    setBusy(true);
    setError(null);
    const answers: QuoteAnswers = {
      ...a,
      date: draft.flexible ? "" : (a.date ?? ""),
      month: draft.flexible ? (a.month ?? "") : "",
      game_master: offered.gameMaster && !!a.game_master,
      extras: (a.extras ?? []).filter((x) => !offered.off.has(x)),
      languages: (a.languages ?? []).filter((l) => !offered.off.has(l)),
    };
    try {
      await sendQuoteRequest(org, { answers, name: draft.name, company: draft.company, email: draft.email, phone: draft.phone, website, started: form.started });
      try {
        sessionStorage.removeItem(storageKey(org));
      } catch {
        // Nothing kept.
      }
      setDraft(blankDraft(locale.startsWith("sv") ? "sv" : "en"));
      history.replaceState({ request: "done" }, "");
      setStep("done");
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 429
          ? t("Too many requests from here just now. Try again in an hour.")
          : err instanceof ApiError && err.status === 400
            ? t("Something is missing or wrong. Check your answers and try again.")
            : t("Something went wrong. Try again."),
      );
    } finally {
      setBusy(false);
    }
  };
  const occasion = OCCASIONS.find((o) => o.id === a.occasion);
  const scope = SCOPES.find((o) => o.id === a.scope);
  const when = [
    draft.flexible ? (a.month ? t("Sometime in {{month}}", { month: monthName(a.month, locale) }) : "") : a.date ? dateName(a.date, locale) : "",
    TIMES.find((o) => o.id === a.time_of_day)?.label,
    DURATIONS.find((o) => o.id === a.duration)?.label,
  ]
    .filter((x): x is string => !!x)
    .map((x, i) => (i === 0 ? t(x) : t(x).toLowerCase()))
    .join(", ");
  const extras = [
    ...EXTRAS.filter((o) => (a.extras ?? []).includes(o.id)).map((o) => t(o.label)),
    ...(a.languages ?? []).map((l) => (l === "sv" ? t("Swedish") : t("English"))),
  ].join(", ");
  const rows: { label: string; value: string; step: Step | null }[] = [
    { label: t("Occasion"), value: occasion ? t(occasion.label) : "", step: "occasion" },
    { label: t("When"), value: when, step: "when" },
    { label: t("People"), value: String(a.people), step: "people" },
    { label: t("Where"), value: [a.city, a.venue, a.help_choose ? t("help choosing where") : ""].filter(Boolean).join(", "), step: "where" },
    { label: t("What"), value: [scope ? t(scope.label) : "", a.game_master ? t("with a game master") : ""].filter(Boolean).join(", "), step: offered.askScope ? "scope" : null },
    { label: t("Also"), value: extras, step: "extras" },
    { label: t("Message"), value: a.message ?? "", step: "extras" },
    { label: t("Budget"), value: a.budget ? budgetLabel(a.budget, form.currency, locale, t) : "", step: "budget" },
    { label: t("You"), value: [draft.name, draft.company, draft.email, draft.phone].filter(Boolean).join(", "), step: "contact" },
  ];
  return shell(
    <>
      {heading(t("Is this right?"), t("Tap an answer to change it."))}
      <ul className="req-summary">
        {rows
          .filter((r) => r.value)
          .map((r) => (
            <li key={r.label}>
              {r.step ? (
                <button type="button" onClick={() => edit(r.step!)}>
                  <span className="req-summary__label">{r.label}</span>
                  <span className="req-summary__value">{r.value}</span>
                  <Icon name="pencil" size={16} />
                </button>
              ) : (
                <div className="req-summary__row">
                  <span className="req-summary__label">{r.label}</span>
                  <span className="req-summary__value">{r.value}</span>
                </div>
              )}
            </li>
          ))}
      </ul>
      {error && (
        <p className="zv-error" role="alert">
          {error}
        </p>
      )}
    </>,
    <button type="button" className="trail-btn trail-btn--primary trail-guide__next" disabled={busy} onClick={send}>
      {busy ? t("Sending…") : t("Send to {{org}}", { org: form.org_name })}
      <Icon name="mail" size={18} />
    </button>,
  );
}

function budgetLabel(id: string, currency: string, locale: string, t: (s: string, v?: Record<string, string | number>) => string): string {
  const k = (n: number) => money(n * 100, currency, locale);
  switch (id) {
    case "under_5k":
      return t("Under {{amount}}", { amount: k(5000) });
    case "5k_15k":
      return `${k(5000)} – ${k(15000)}`;
    case "15k_40k":
      return `${k(15000)} – ${k(40000)}`;
    case "over_40k":
      return t("Over {{amount}}", { amount: k(40000) });
    default:
      return t("Don't know yet");
  }
}

/** One of a few, as big tiles with a line each. */
function Choices({ options, value, onChange }: { options: { id: string; label: string; hint?: string; icon?: string }[]; value: string; onChange: (id: string) => void }) {
  return (
    <ul className="req-choices" role="radiogroup">
      {options.map((o) => (
        <li key={o.id}>
          <button type="button" role="radio" aria-checked={value === o.id} className={(value === o.id ? "is-on" : "") + (o.icon ? " has-icon" : "")} onClick={() => onChange(o.id)}>
            {o.icon && (
              <span className="req-tile__icon" aria-hidden="true">
                <Icon name={o.icon} size={22} />
              </span>
            )}
            <span className="req-tile__text">
              <strong>{o.label}</strong>
              {o.hint && <small>{o.hint}</small>}
            </span>
            <span className="req-tile__tick" aria-hidden="true">
              <Icon name="check" size={16} />
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

/** One of a few short ones, in a row. */
function Chips({ options, value, onChange }: { options: { id: string; label: string }[]; value: string; onChange: (id: string) => void }) {
  return (
    <div className="req-chips" role="radiogroup">
      {options.map((o) => (
        <button key={o.id} type="button" role="radio" aria-checked={value === o.id} className={value === o.id ? "is-on" : ""} onClick={() => onChange(value === o.id ? "" : o.id)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}
