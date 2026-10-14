import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { ApiError } from "@zonventure/api";
import { Check, CircleAlert, Copy, ExternalLink, Globe, Lock, Mail, Palette, RefreshCw, Sparkles, Trash2 } from "lucide-react";
import { FONTS, loadFont } from "@zonventure/play-ui";
import { emails } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import { Button, EmailPreview, ErrorBox, Field, ICON, Loading, errorText, fmtMoney, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { checkDomain, deleteDomain, getDomain, getDomainBrand, getGuide, putDomain, putDomainBrand, sendInstructions, type DomainBrand, type DomainInfo, type DomainState } from "./domain-api.ts";
import { getContact, getProfile, putContact, putProfile, type OrgContact, type OrgProfile } from "./organization-api.ts";
import { colorsFromLogo, contrast, fixContrast, type BrandColors } from "./brandColors.ts";
import "./domain.css";

/**
 * Settings → Domain: an organisation's store on its own address, set up
 * in four steps — choose the address, add one DNS record, we check it,
 * make it yours — beside a preview of how it will look. Without the
 * feature, what it is and how to get it. See internal/admin/domains.go.
 */
export function DomainTab() {
  const t = useT();
  const load = useLoad((signal) => getDomain(signal), []);
  const profile = useLoad((signal) => getProfile(signal), []);
  const details = useLoad((signal) => getDomainBrand(signal), []);
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const canEdit = user?.role === "org_admin";
  const orgID = user?.org_id ?? 0;

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const d = load.data;
  if (!d.enabled) {
    return (
      <div className="dm">
        <div className="dm-empty">
          <Globe size={28} aria-hidden />
          <p>{t.t("This server doesn't offer custom domains. Its operator turns them on with ZV_DOMAIN_CNAME_SUFFIX.")}</p>
        </div>
      </div>
    );
  }
  const brand = brandOf(profile.data ?? undefined, orgID, details.data ?? undefined);
  if (!d.entitled) return <Offer d={d} brand={brand} />;
  return (
    <Setup
      d={d}
      set={load.set}
      brand={brand}
      profile={profile.data ?? null}
      profileError={profile.error}
      reloadProfile={profile.reload}
      setProfile={profile.set}
      details={details.data ?? null}
      setDetails={details.set}
      canEdit={canEdit}
    />
  );
}

/** What the preview shows: the organisation's colours and logo. */
interface Brand {
  accent: string;
  accent2: string;
  surface: string;
  ink: string;
  logo: string;
  tagline: string;
  /** The title typeface's family, "" for the site's. */
  font: string;
  /** The name it goes by on its domain, "" for its own. */
  name: string;
}

const DEFAULT_BRAND = { accent: "#ad1357", accent2: "#e24080", surface: "#fbfbfd", ink: "#1f3349" };
const HEX = /^#[0-9a-fA-F]{6}$/;

function brandOf(p: OrgProfile | undefined, orgID: number, b?: DomainBrand): Brand {
  const pick = (v: string | undefined, def: string) => (v && HEX.test(v) ? v : def);
  return {
    accent: pick(p?.accent_color, DEFAULT_BRAND.accent),
    accent2: pick(p?.accent_secondary_color, DEFAULT_BRAND.accent2),
    surface: pick(p?.background_color, DEFAULT_BRAND.surface),
    ink: pick(p?.text_color, DEFAULT_BRAND.ink),
    logo: p?.has_logo && orgID ? `/org-logo/${orgID}?v=${p.accent_color ?? ""}` : "",
    tagline: p?.tagline ?? "",
    font: (b?.font && FONTS[b.font]) || "",
    name: b?.display_name ?? "",
  };
}

// --- without the feature ----------------------------------------------------

function Offer({ d, brand }: { d: DomainInfo; brand: Brand }) {
  const t = useT();
  const example = `${d.suggestion}.${t.t("yourbrand.com")}`;
  const price = d.price_year_cents > 0
    ? t.t("{{price}} per year", { price: fmtMoney(d.price_year_cents, d.currency, t.current()) }) + " · " + (d.vat_excluded ? t.t("excl. VAT") : t.t("incl. VAT"))
    : "";
  return (
    <div className="dm">
      <section className="dm-hero">
        <div className="dm-hero__text">
          <span className="dm-eyebrow"><Sparkles size={ICON.sm} aria-hidden /> {t.t("Your own domain")}</span>
          <h2>{t.t("Your adventures on your own address")}</h2>
          <p>{t.t("Give players {{example}} instead of our address. Your store, tickets and games live there, in your colours and under your name only.", { example })}</p>
          <ul className="dm-benefits">
            <li><Check size={ICON.md} aria-hidden /> {t.t("Your address in every ticket, invitation, email and printed letter")}</li>
            <li><Check size={ICON.md} aria-hidden /> {t.t("Your logo, colours and name everywhere players look, and nobody else's")}</li>
            <li><Check size={ICON.md} aria-hidden /> {t.t("Set up in minutes with one DNS record; we look after the security certificate")}</li>
            <li><Check size={ICON.md} aria-hidden /> {t.t("Links you've already sent keep working")}</li>
          </ul>
          <div className="dm-offer">
            {price && <span className="dm-price">{price}</span>}
            {d.contact_url ? (
              <a className="dm-cta" href={d.contact_url} target="_blank" rel="noopener">
                <Mail size={ICON.md} aria-hidden /> {price ? t.t("Get your own domain") : t.t("Contact us")}
              </a>
            ) : (
              <span className="dm-muted">{t.t("Ask the platform's operator to turn it on for your organization.")}</span>
            )}
          </div>
          {d.until && !d.entitled && (
            <p className="dm-muted">{t.t("Your own domain ended on {{date}}. Renew it to switch it back on.", { date: d.until })}</p>
          )}
        </div>
        <StorePreview host={example} name={brand.name || d.org_name} brand={brand} ghost />
      </section>
    </div>
  );
}

// --- setting it up ----------------------------------------------------------

function Setup(props: {
  d: DomainInfo;
  set: (d: DomainInfo) => void;
  brand: Brand;
  profile: OrgProfile | null;
  profileError: string | null | undefined;
  reloadProfile: () => void;
  setProfile: (p: OrgProfile) => void;
  details: DomainBrand | null;
  setDetails: (b: DomainBrand) => void;
  canEdit: boolean;
}) {
  const { d, set, brand, canEdit } = props;
  // What the address field holds, so the preview follows the typing.
  const [typed, setTyped] = useState("");
  const t = useT();
  const dialogs = useDialogs();
  const toast = useToast();
  const dom = d.domain;
  const live = dom?.status === "live";
  const [editing, setEditing] = useState(!dom);
  useEffect(() => {
    if (!d.domain) setEditing(true);
  }, [d.domain]);

  // Check by itself while the record isn't right yet, the page open.
  const [checking, setChecking] = useState(false);
  const runCheck = async (quiet: boolean) => {
    setChecking(true);
    try {
      set(await checkDomain());
    } catch (e) {
      if (!quiet) toast.err(errorText(e));
    } finally {
      setChecking(false);
    }
  };
  const pending = !!dom && !live;
  useEffect(() => {
    if (!pending) return;
    void runCheck(true);
    const id = window.setInterval(() => void runCheck(true), 30000);
    return () => window.clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pending, dom?.host]);

  const remove = async () => {
    if (!dom) return;
    const ok = await dialogs.confirm({
      title: t.t("Stop using {{host}}?", { host: dom.host }),
      message: t.t("Your store goes back to our address. Links already sent with {{host}} keep working only while its DNS record still points at us.", { host: dom.host }),
      confirmLabel: t.t("Stop using it"),
      danger: true,
    });
    if (!ok) return;
    try {
      set(await deleteDomain());
      toast.ok(t.t("Your store is back on our address"));
    } catch (e) {
      toast.err(errorText(e));
    }
  };

  const brandDone = !!props.profile?.has_logo && !!props.profile?.accent_color && !!props.details?.font;
  const steps: { key: string; title: string; done: boolean; body: ReactNode }[] = [
    {
      key: "address",
      title: dom && !editing ? t.t("Address: {{host}}", { host: dom.host }) : t.t("Choose the address"),
      done: !!dom && !editing,
      body: <AddressStep d={d} set={set} canEdit={canEdit} editing={editing} setEditing={setEditing} onTyped={setTyped} />,
    },
    {
      key: "record",
      title: t.t("Add one DNS record"),
      done: !!dom && (dom.status === "certificate" || live),
      body: !dom ? <p className="dm-muted">{t.t("Choose the address first.")}</p>
        : live && !dom.down_since ? (
          <details className="dm-done">
            <summary>{t.t("CNAME {{name}} → {{value}}", { name: dom.name, value: dom.value })}</summary>
            <RecordStep d={d} dom={dom} canEdit={canEdit} />
          </details>
        ) : <RecordStep d={d} dom={dom} canEdit={canEdit} />,
    },
    {
      key: "check",
      title: live ? t.t("It works") : t.t("We check it"),
      done: live,
      body: dom ? <CheckStep dom={dom} checking={checking} onCheck={() => void runCheck(false)} /> : <p className="dm-muted">{t.t("We check it as soon as there's an address.")}</p>,
    },
    {
      key: "brand",
      title: t.t("Make it yours"),
      done: brandDone,
      body: props.profileError && !props.profile
        ? <ErrorBox message={props.profileError} retry={props.reloadProfile} retryLabel={t.t("Retry")} />
        : <BrandStep profile={props.profile} setProfile={props.setProfile} details={props.details} setDetails={props.setDetails} brand={brand} canEdit={canEdit} />,
    },
  ];
  const current = steps.findIndex((s) => !s.done);

  return (
    <div className="dm">
      {d.in_grace && (
        <div className="dm-banner dm-banner--warn" role="status">
          <CircleAlert size={ICON.lg} aria-hidden />
          <div>
            <strong>{t.t("Your own domain ended on {{date}}.", { date: d.until ?? "" })}</strong>{" "}
            {t.t("It keeps working until {{date}}; then links to it go to your store on our address.", { date: d.grace_until ?? "" })}{" "}
            {d.contact_url && <a href={d.contact_url} target="_blank" rel="noopener">{t.t("Renew")}</a>}
          </div>
        </div>
      )}
      {live && dom && (
        <section className="dm-live">
          <div className="dm-live__burst" aria-hidden>
            <Sparkles size={22} />
          </div>
          <div className="dm-live__text">
            <h2>{t.t("{{host}} is live", { host: dom.host })}</h2>
            <p>{t.t("Players see only your name there. Every ticket, invitation and printed letter links to it.")}</p>
            {dom.down_since && (
              <p className="dm-live__warn"><CircleAlert size={ICON.sm} aria-hidden /> {t.t("We haven't reached it since {{when}}. Check the DNS record below.", { when: new Date(dom.down_since).toLocaleString(t.current()) })}</p>
            )}
          </div>
          <a className="dm-cta" href={origin(dom.host)} target="_blank" rel="noopener">
            {t.t("Open my store")} <ExternalLink size={ICON.sm} aria-hidden />
          </a>
        </section>
      )}
      <div className="dm-layout">
        <ol className="dm-steps">
          {steps.map((s, i) => {
            const state = s.done ? "done" : i === current ? "current" : "todo";
            return (
              <li key={s.key} className={`dm-step is-${state}`} aria-current={state === "current" ? "step" : undefined}>
                <div className="dm-step__marker" aria-hidden>{s.done ? <Check size={ICON.md} /> : i + 1}</div>
                <div className="dm-step__main">
                  <h3 className="dm-step__title">{s.title}</h3>
                  <div className="dm-step__body">{s.body}</div>
                </div>
              </li>
            );
          })}
        </ol>
        <aside className="dm-aside">
          <Previews host={(editing && typed) || dom?.host || `${d.suggestion}.${t.t("yourbrand.com")}`} name={brand.name || d.org_name} brand={brand} live={live} ghost={!dom} />
          {d.until && (
            <p className="dm-muted dm-aside__note">{d.forever ? t.t("Included, with no end date.") : t.t("Included until {{date}}.", { date: d.until })}</p>
          )}
          {dom && canEdit && (
            <Button variant="ghost" size="sm" icon={<Trash2 size={ICON.sm} />} onClick={() => void remove()}>
              {t.t("Stop using this domain")}
            </Button>
          )}
        </aside>
      </div>
    </div>
  );
}

/** host's address: https, but plain http with this page's port for a development *.localhost name. */
function origin(host: string): string {
  return host.endsWith(".localhost") ? `http://${host}${location.port ? ":" + location.port : ""}` : `https://${host}`;
}

// --- step 1 -------------------------------------------------------------------

function splitHost(host: string): [string, string] {
  const i = host.indexOf(".");
  return i < 0 ? [host, ""] : [host.slice(0, i), host.slice(i + 1)];
}

function AddressStep({ d, set, canEdit, editing, setEditing, onTyped }: {
  d: DomainInfo;
  set: (d: DomainInfo) => void;
  canEdit: boolean;
  editing: boolean;
  setEditing: (v: boolean) => void;
  onTyped: (host: string) => void;
}) {
  const t = useT();
  const toast = useToast();
  const [sub, setSub] = useState(d.domain ? splitHost(d.domain.host)[0] : d.suggestion);
  const [apex, setApex] = useState(d.domain ? splitHost(d.domain.host)[1] : "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const apexRef = useRef<HTMLInputElement>(null);
  const cleanApex = apex.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/^www\./, "").replace(/\/.*$/, "");
  const host = `${sub.trim().toLowerCase()}.${cleanApex || t.t("yourbrand.com")}`;
  useEffect(() => {
    onTyped(editing ? host : "");
  }, [host, editing, onTyped]);

  if (d.domain && !editing) {
    return (
      <div className="dm-row">
        <span className="dm-host"><Globe size={ICON.md} aria-hidden /> {d.domain.host}</span>
        {canEdit && <Button variant="link" size="sm" onClick={() => setEditing(true)}>{t.t("Change")}</Button>}
      </div>
    );
  }

  const save = async () => {
    setErr("");
    if (!cleanApex) {
      setErr(t.t("Enter your website's domain, like yourbrand.com."));
      apexRef.current?.focus();
      return;
    }
    setBusy(true);
    try {
      set(await putDomain(host));
      setEditing(false);
      toast.ok(t.t("Saved"));
    } catch (e) {
      const reason = e instanceof ApiError ? (e.body as { reason?: string } | undefined)?.reason : undefined;
      setErr(reasonText(t, reason, cleanApex) || errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="dm-address" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <p className="dm-muted">{t.t("Pick a name in front of your own domain. Your main website stays exactly as it is.")}</p>
      <div className="dm-host-input" aria-invalid={!!err}>
        <label className="dm-host-input__sub">
          <span className="cs-sr">{t.t("Name in front")}</span>
          <input value={sub} disabled={!canEdit} onChange={(e) => setSub(e.target.value.replace(/[^a-zA-Z0-9-]/g, ""))} maxLength={63} spellCheck={false} />
        </label>
        <span className="dm-host-input__dot">.</span>
        <label className="dm-host-input__apex">
          <span className="cs-sr">{t.t("Your domain")}</span>
          <input ref={apexRef} value={apex} disabled={!canEdit} placeholder={t.t("yourbrand.com")} onChange={(e) => setApex(e.target.value)} spellCheck={false} autoCapitalize="off" />
        </label>
      </div>
      <div className="dm-chips" role="group" aria-label={t.t("Suggestions")}>
        {[d.suggestion, "play", "adventures", "aventyr", "quest"].filter((v, i, a) => a.indexOf(v) === i).map((s) => (
          <button type="button" key={s} className={"dm-chip" + (s === sub ? " is-on" : "")} disabled={!canEdit} onClick={() => setSub(s)}>{s}</button>
        ))}
      </div>
      {err && <p className="dm-err" role="alert">{err}</p>}
      {canEdit ? (
        <div className="dm-row">
          <Button type="submit" variant="primary" loading={busy} disabled={!sub.trim()}>{cleanApex ? t.t("Use {{host}}", { host }) : t.t("Use this address")}</Button>
          {d.domain && <Button variant="ghost" onClick={() => setEditing(false)}>{t.t("Cancel")}</Button>}
        </div>
      ) : (
        <p className="dm-muted">{t.t("An admin of your organization sets this up.")}</p>
      )}
    </form>
  );
}

function reasonText(t: ReturnType<typeof useT>, reason: string | undefined, apex: string): string {
  switch (reason) {
    case "apex":
      return t.t("Use a name in front, like adventures.{{apex}}: your main website keeps working on {{apex}}.", { apex });
    case "www":
      return t.t("www is usually your main website. Choose another name, like adventures.");
    case "ours":
      return t.t("That's one of our own addresses. Use your organization's domain.");
    case "invalid":
      return t.t("That isn't a domain we can use. Check for typos: letters, digits and hyphens only.");
    case "taken":
      return t.t("Another organization already uses that address.");
    case "not_entitled":
      return t.t("Your organization doesn't have its own domain right now.");
  }
  return "";
}

// --- step 2 -------------------------------------------------------------------

function CopyValue({ value, label }: { value: string; label: string }) {
  const t = useT();
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setDone(true);
      window.setTimeout(() => setDone(false), 1600);
    } catch {
      // The value is on screen to select by hand.
    }
  };
  return (
    <button type="button" className={"dm-copy" + (done ? " is-done" : "")} onClick={() => void copy()} aria-label={t.t("Copy {{what}}", { what: label })}>
      {done ? <Check size={ICON.sm} aria-hidden /> : <Copy size={ICON.sm} aria-hidden />}
      <span>{done ? t.t("Copied") : t.t("Copy")}</span>
    </button>
  );
}

/** A hostname that wraps after its dots, not inside a word. */
function breakable(host: string): ReactNode {
  return host.split(".").map((part, i, all) => (
    <Fragment key={i}><span>{part}</span>{i < all.length - 1 && <>.<wbr /></>}</Fragment>
  ));
}

/** The guides' few Markdown marks: a numbered list, **bold**, `code`, *italic*. */
function Steps({ md }: { md: string }) {
  const inline = (s: string): ReactNode[] =>
    s.split(/(\*\*[^*]+\*\*|`[^`]+`|\*[^*]+\*)/g).filter(Boolean).map((p, i) =>
      p.startsWith("**") ? <strong key={i}>{p.slice(2, -2)}</strong> : p.startsWith("`") ? <code key={i}>{breakable(p.slice(1, -1))}</code> : p.startsWith("*") ? <em key={i}>{p.slice(1, -1)}</em> : <Fragment key={i}>{p}</Fragment>,
    );
  const items = md.split("\n").map((l) => l.replace(/^\d+\.\s*/, "").trim()).filter(Boolean);
  return <ol className="dm-guide">{items.map((l, i) => <li key={i}>{inline(l)}</li>)}</ol>;
}

function RecordStep({ d, dom, canEdit }: { d: DomainInfo; dom: DomainState; canEdit: boolean }) {
  const t = useT();
  const toast = useToast();
  const [provider, setProvider] = useState("");
  const guide = useLoad(() => (provider ? getGuide(provider, t.current()) : Promise.resolve("")), [provider, dom.host, t.current()]);
  const [showSend, setShowSend] = useState(false);
  const [email, setEmail] = useState("");
  const [sending, setSending] = useState(false);
  const send = async () => {
    setSending(true);
    try {
      const r = await sendInstructions(email.trim(), provider, t.current());
      toast.ok(t.t("Sent to {{email}}", { email: r.sent_to }));
      setShowSend(false);
      setEmail("");
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setSending(false);
    }
  };
  return (
    <div className="dm-record">
      <p className="dm-muted">{t.t("Sign in where you manage {{apex}} and add this record. It's the only change needed.", { apex: dom.apex })}</p>
      <div className="dm-table" role="table" aria-label={t.t("DNS record")}>
        <div className="dm-table__row" role="row">
          <span className="dm-table__k" role="rowheader">{t.t("Type")}</span>
          <code className="dm-table__v" role="cell">CNAME</code>
          <span />
        </div>
        <div className="dm-table__row" role="row">
          <span className="dm-table__k" role="rowheader">{t.t("Name")}</span>
          <code className="dm-table__v" role="cell">{breakable(dom.name)}</code>
          <CopyValue value={dom.name} label={t.t("Name")} />
        </div>
        <div className="dm-table__row" role="row">
          <span className="dm-table__k" role="rowheader">{t.t("Value")}</span>
          <code className="dm-table__v" role="cell">{breakable(dom.value)}</code>
          <CopyValue value={dom.value} label={t.t("Value")} />
        </div>
      </div>
      <p className="dm-hint">{t.t("Some panels call the name Host or Alias and the value Target, Points to or Data. TTL can stay as it is.")}</p>

      <div className="dm-providers">
        <span className="dm-label">{t.t("Where is your domain? Pick it for exact steps:")}</span>
        <div className="dm-chips" role="group" aria-label={t.t("Domain providers")}>
          {d.providers.map((p) => (
            <button type="button" key={p.id} className={"dm-chip" + (p.id === provider ? " is-on" : "")} aria-pressed={p.id === provider} onClick={() => setProvider(p.id === provider ? "" : p.id)}>
              {p.id === "other" ? t.t("Somewhere else") : p.name}
            </button>
          ))}
        </div>
        {provider && (guide.data ? <Steps md={guide.data} /> : guide.error ? <p className="dm-err">{guide.error}</p> : <Loading label={t.t("Loading…")} />)}
      </div>

      {canEdit && (
        <div className="dm-send">
          {showSend ? (
            <form className="dm-send__form" onSubmit={(e) => { e.preventDefault(); void send(); }}>
              <Field label={t.t("Someone else runs your domain? Send them everything they need.")} hint={t.t("They get the record, the steps for the provider you picked, and a page that shows when it works. No account needed.")}>
                <input type="email" required value={email} placeholder={t.t("it@yourbrand.com")} onChange={(e) => setEmail(e.target.value)} autoFocus />
              </Field>
              <div className="dm-row">
                <Button type="submit" variant="primary" loading={sending} icon={<Mail size={ICON.sm} />}>{t.t("Send instructions")}</Button>
                <Button variant="ghost" onClick={() => setShowSend(false)}>{t.t("Cancel")}</Button>
              </div>
            </form>
          ) : (
            <Button variant="link" size="sm" icon={<Mail size={ICON.sm} />} onClick={() => setShowSend(true)}>
              {t.t("Send these instructions to someone else")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

// --- step 3 -------------------------------------------------------------------

function CheckStep({ dom, checking, onCheck }: { dom: DomainState; checking: boolean; onCheck: () => void }) {
  const t = useT();
  const fix: Record<string, string> = {
    doubled: t.t("The name was entered as the full address, so it became {{host}}.{{apex}}. Edit the record and enter only {{name}} as its name.", { host: dom.host, apex: dom.apex, name: dom.name }),
    address: t.t("There's an A or AAAA record for {{name}}. Remove it, then add the CNAME record above.", { name: dom.name }),
    cloudflare: t.t("Cloudflare's proxy is on for this record. Click the orange cloud so it turns grey (DNS only)."),
    other_target: t.t("The record points somewhere else. Change its value to {{value}}.", { value: dom.value }),
    certificate: t.t("The record is right and reaches us, but the secure connection isn't answering yet. This usually sorts itself out within minutes."),
  };
  const text =
    dom.status === "live" ? t.t("Everything works. Your store answers on https://{{host}}.", { host: dom.host })
    : dom.status === "certificate" ? (dom.problem === "certificate" ? fix.certificate : t.t("The record is right. Getting a security certificate…"))
    : dom.status === "wrong" ? t.t("We found a record, but it isn't right yet.")
    : t.t("Waiting for the record. Changes usually show within a few minutes, at most a few hours.");
  const [, tick] = useState(0);
  useEffect(() => {
    const id = window.setInterval(() => tick((n) => n + 1), 10000);
    return () => window.clearInterval(id);
  }, []);
  return (
    <div className="dm-check">
      <div className={`dm-status is-${dom.status}`} role="status" aria-live="polite">
        <span className="dm-status__dot" aria-hidden />
        <div>
          <div className="dm-status__text">{text}</div>
          {dom.status === "wrong" && dom.problem && fix[dom.problem] && <div className="dm-status__fix">{fix[dom.problem]}</div>}
          {dom.seen && dom.status !== "live" && <div className="dm-status__seen">{t.t("We see:")} <code>{dom.seen}</code></div>}
        </div>
      </div>
      {dom.status !== "live" && (
        <div className="dm-row">
          <Button size="sm" icon={<RefreshCw size={ICON.sm} className={checking ? "dm-spin" : ""} />} onClick={onCheck} disabled={checking}>
            {checking ? t.t("Checking…") : t.t("Check now")}
          </Button>
          <span className="dm-muted">
            {dom.checked_at ? t.t("Checked {{ago}}. We keep checking while this page is open, and every 10 minutes after.", { ago: ago(t, dom.checked_at) }) : t.t("We keep checking while this page is open, and every 10 minutes after.")}
          </span>
        </div>
      )}
    </div>
  );
}

function ago(t: ReturnType<typeof useT>, iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (s < 10) return t.t("just now");
  if (s < 60) return t.t("{{n}} seconds ago", { n: String(s) });
  const m = Math.round(s / 60);
  if (m < 60) return m === 1 ? t.t("a minute ago") : t.t("{{n}} minutes ago", { n: String(m) });
  return new Date(iso).toLocaleString(t.current());
}

// --- step 4 -------------------------------------------------------------------

function BrandStep({ profile, setProfile, details, setDetails, brand, canEdit }: {
  profile: OrgProfile | null;
  setProfile: (p: OrgProfile) => void;
  details: DomainBrand | null;
  setDetails: (b: DomainBrand) => void;
  brand: Brand;
  canEdit: boolean;
}) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [tagline, setTagline] = useState("");
  if (!profile) return <Loading label={t.t("Loading…")} />;
  const hasColors = !!profile.accent_color;
  const colors = (): BrandColors => ({
    accent_color: profile.accent_color ?? "",
    accent_secondary_color: profile.accent_secondary_color ?? "",
    background_color: profile.background_color ?? "",
    text_color: profile.text_color ?? "",
  });
  const saveTagline = async () => {
    setBusy(true);
    try {
      setProfile(await putProfile({ tagline: tagline.trim(), public_website: profile.public_website, ...colors() }));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const textOk = contrast(brand.ink, brand.surface) >= 4.5;
  const buttonOk = contrast("#ffffff", brand.accent) >= 3;

  const apply = async (c: BrandColors) => {
    setBusy(true);
    try {
      setProfile(await putProfile({ tagline: profile.tagline, public_website: profile.public_website, ...c }));
      toast.ok(t.t("Colours updated"));
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const fromLogo = async () => {
    try {
      await apply(await colorsFromLogo(brand.logo));
    } catch (e) {
      toast.err(errorText(e));
    }
  };
  const fixColors = () =>
    void apply({
      accent_color: buttonOk ? brand.accent : fixContrast(brand.accent, "#ffffff", 3),
      accent_secondary_color: brand.accent2,
      background_color: brand.surface,
      text_color: textOk ? brand.ink : fixContrast(brand.ink, brand.surface),
    });
  const toBranding = () => navigate({ name: "settings", tab: "organization" });

  const items: { done: boolean; label: string; hint: string; action?: ReactNode }[] = [
    {
      done: profile.has_logo,
      label: t.t("Your logo"),
      hint: profile.has_logo ? t.t("On your store, its tab and every email.") : t.t("Upload it once: it becomes your store's header, its tab icon and your emails' signature."),
      action: !profile.has_logo && canEdit ? <Button size="sm" onClick={toBranding}>{t.t("Upload logo")}</Button> : undefined,
    },
    {
      done: hasColors && textOk && buttonOk,
      label: t.t("Your colours"),
      hint: !hasColors
        ? profile.has_logo ? t.t("We can pick them from your logo.") : t.t("Choose them, or upload a logo and we'll pick them from it.")
        : !textOk || !buttonOk ? t.t("Some text would be hard to read on these colours.") : t.t("Buttons, links and backgrounds use them."),
      action: canEdit ? (
        !hasColors && profile.has_logo ? <Button size="sm" variant="primary" loading={busy} icon={<Palette size={ICON.sm} />} onClick={() => void fromLogo()}>{t.t("Pick from my logo")}</Button>
        : hasColors && (!textOk || !buttonOk) ? <Button size="sm" variant="primary" loading={busy} onClick={fixColors}>{t.t("Fix contrast")}</Button>
        : profile.has_logo ? <Button size="sm" variant="ghost" loading={busy} icon={<Palette size={ICON.sm} />} onClick={() => void fromLogo()}>{t.t("Pick again from my logo")}</Button>
        : undefined
      ) : undefined,
    },
    {
      done: !!profile.tagline.trim(),
      label: t.t("A tagline"),
      hint: profile.tagline.trim() ? `“${profile.tagline.trim()}”` : t.t("One line under your name that says what you offer."),
      action: !profile.tagline.trim() && canEdit ? (
        <form className="dm-inline" onSubmit={(e) => { e.preventDefault(); void saveTagline(); }}>
          <input value={tagline} maxLength={200} placeholder={t.t("Adventures in your city")} aria-label={t.t("A tagline")} onChange={(e) => setTagline(e.target.value)} />
          <Button type="submit" size="sm" variant="primary" loading={busy} disabled={!tagline.trim()}>{t.t("Add tagline")}</Button>
        </form>
      ) : undefined,
    },
  ];
  return (
    <div className="dm-brand">
      <ul className="dm-checklist">
        {items.map((it) => (
          <li key={it.label} className={it.done ? "is-done" : ""}>
            <span className="dm-checklist__mark" aria-hidden>{it.done ? <Check size={ICON.sm} /> : null}</span>
            <div className="dm-checklist__text">
              <strong>{it.label}</strong>
              <span>{it.hint}</span>
            </div>
            {it.action}
          </li>
        ))}
      </ul>
      {hasColors && (
        <div className="dm-swatches" aria-label={t.t("Your colours")}>
          {[brand.accent, brand.accent2, brand.surface, brand.ink].map((c, i) => <span key={i} style={{ background: c }} title={c} />)}
        </div>
      )}
      {details && <BrandDetails details={details} setDetails={setDetails} brand={brand} canEdit={canEdit} />}
      <p className="dm-muted">
        {t.t("On your own domain players see only your name: no mention of us in pages, titles or emails. Emails go out under your name, and replies reach your contact address.")}{" "}
        {canEdit && <Button variant="link" size="sm" onClick={toBranding}>{t.t("All branding settings")}</Button>}
      </p>
    </div>
  );
}

/** Fonts for a title: those the player ships, by key. */
const FONT_KEYS = Object.keys(FONTS).sort((a, b) => FONTS[a]!.localeCompare(FONTS[b]!));

/**
 * What the organisation goes by on its own domain: the name, its title
 * typeface, the address players' replies reach, and links to its own
 * terms and privacy policy, shown beside ours. Saved together.
 */
function BrandDetails({ details, setDetails, brand, canEdit }: {
  details: DomainBrand;
  setDetails: (b: DomainBrand) => void;
  brand: Brand;
  canEdit: boolean;
}) {
  const t = useT();
  const toast = useToast();
  const contact = useLoad((s) => getContact(s), []);
  const [edit, setEdit] = useState<DomainBrand>(details);
  const [email, setEmail] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => setEdit(details), [details]);
  useEffect(() => {
    if (contact.data && email === null) setEmail(contact.data.contact_email);
  }, [contact.data, email]);
  useEffect(() => {
    for (const k of FONT_KEYS) loadFont(k);
  }, []);
  const c: OrgContact | undefined = contact.data ?? undefined;
  const changed = JSON.stringify(edit) !== JSON.stringify(details) || (c !== undefined && email !== null && email.trim() !== c.contact_email);
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      setDetails(await putDomainBrand({ ...edit, display_name: edit.display_name.trim(), terms_url: edit.terms_url.trim(), privacy_url: edit.privacy_url.trim() }));
      if (c && email !== null && email.trim() !== c.contact_email) contact.set(await putContact({ contact_email: email.trim(), time_zone: c.time_zone }));
      toast.ok(t.t("Saved"));
    } catch (e) {
      const reason = e instanceof ApiError ? (e.body as { reason?: string } | undefined)?.reason : undefined;
      setErr(
        reason === "invalid_url" ? t.t("Links must be full addresses starting with https://.")
        : reason === "invalid_email" ? t.t("That isn't an email address.")
        : errorText(e),
      );
    } finally {
      setBusy(false);
    }
  };
  const set = (k: keyof DomainBrand) => (e: { target: { value: string } }) => setEdit({ ...edit, [k]: e.target.value });
  return (
    <form className="dm-details" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <h4 className="dm-details__title">{t.t("How you appear")}</h4>
      <div className="dm-details__grid">
        <Field label={t.t("Name on your domain")} hint={t.t("Page titles, the header and the sender of your emails. Empty uses your organization's name.")}>
          <input value={edit.display_name} disabled={!canEdit} maxLength={100} placeholder={brand.name || t.t("Your organization's name")} onChange={set("display_name")} />
        </Field>
        <Field label={t.t("Replies go to")} hint={c ? t.t("Empty uses your first admin's address: {{email}}", { email: c.default_contact_email || "—" }) : undefined}>
          <input type="email" value={email ?? ""} disabled={!canEdit || !c} maxLength={254} placeholder={c?.default_contact_email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
      </div>
      <div className="dm-label">{t.t("Title typeface")}</div>
      <div className="dm-fonts" role="radiogroup" aria-label={t.t("Title typeface")}>
        <button type="button" role="radio" aria-checked={!edit.font} className={"dm-font" + (!edit.font ? " is-on" : "")} disabled={!canEdit} onClick={() => setEdit({ ...edit, font: "" })}>
          <span className="dm-font__sample">Aa</span>
          <span className="dm-font__name">{t.t("Site default")}</span>
        </button>
        {FONT_KEYS.map((k) => (
          <button type="button" role="radio" aria-checked={edit.font === k} key={k} className={"dm-font" + (edit.font === k ? " is-on" : "")} disabled={!canEdit} onClick={() => setEdit({ ...edit, font: k })}>
            <span className="dm-font__sample" style={{ fontFamily: `"${FONTS[k]}", var(--dz-font-sans)` }}>Aa</span>
            <span className="dm-font__name">{FONTS[k]}</span>
          </button>
        ))}
      </div>
      <div className="dm-details__grid">
        <Field label={t.t("Your terms (optional)")} hint={t.t("Shown in your store's footer, beside the platform's.")}>
          <input type="url" value={edit.terms_url} disabled={!canEdit} maxLength={500} placeholder="https://" onChange={set("terms_url")} />
        </Field>
        <Field label={t.t("Your privacy policy (optional)")}>
          <input type="url" value={edit.privacy_url} disabled={!canEdit} maxLength={500} placeholder="https://" onChange={set("privacy_url")} />
        </Field>
      </div>
      {err && <p className="dm-err" role="alert">{err}</p>}
      {canEdit && <div className="dm-row"><Button type="submit" variant="primary" loading={busy} disabled={!changed}>{t.t("Save")}</Button></div>}
    </form>
  );
}

// --- the previews --------------------------------------------------------------

/** The store, a ticket email and a player's screen, as they will look. */
function Previews(props: { host: string; name: string; brand: Brand; live?: boolean; ghost?: boolean }) {
  const t = useT();
  const [tab, setTab] = useState<"store" | "email" | "player">("store");
  const tabs = [
    { id: "store" as const, label: t.t("Store") },
    { id: "email" as const, label: t.t("Ticket email") },
    { id: "player" as const, label: t.t("Player screen") },
  ];
  return (
    <div className="dm-previews">
      <div className="dm-tabs" role="tablist" aria-label={t.t("Preview")}>
        {tabs.map((x) => (
          <button type="button" role="tab" key={x.id} aria-selected={tab === x.id} className={"dm-tab" + (tab === x.id ? " is-on" : "")} onClick={() => setTab(x.id)}>
            {x.label}
          </button>
        ))}
      </div>
      {tab === "store" && (
        <>
          <StorePreview {...props} />
          <p className="dm-muted dm-aside__note">
            {props.live ? t.t("This is your store's front page, as players see it.") : t.t("A preview of your store on its own address.")}
          </p>
        </>
      )}
      {tab === "email" && <TicketEmailPreview name={props.name} live={!!props.live} />}
      {tab === "player" && (
        <>
          <PlayerPreview {...props} />
          <p className="dm-muted dm-aside__note">{t.t("A step of an adventure on a player's phone: your colours, your title typeface, your address.")}</p>
        </>
      )}
    </div>
  );
}

/** The real ticket email, rendered by the server in the organisation's look. */
function TicketEmailPreview({ name, live }: { name: string; live: boolean }) {
  const t = useT();
  const load = useLoad((signal) => emails.preview("team_link", { lang: t.current(), signal }), [t.current(), name, live]);
  return (
    <div className="dm-mail">
      <EmailPreview preview={load.data ?? null} loading={!load.data} from={name} />
      <p className="dm-muted dm-aside__note">
        {live ? t.t("The email a buyer gets with their ticket, sent under your name.") : t.t("Once your domain is live, the line about us under the email goes and its links use your address.")}
      </p>
    </div>
  );
}

/** A player's phone mid-adventure, in the organisation's colours and typeface. */
function PlayerPreview({ host, name, brand }: { host: string; name: string; brand: Brand }) {
  const t = useT();
  const title = brand.font ? { fontFamily: `"${brand.font}", var(--dz-font-sans)` } : undefined;
  return (
    <figure className="dm-phone" aria-label={t.t("Preview of a player's screen")}>
      <div className="dm-phone__notch" aria-hidden />
      <div className="dm-phone__url"><Lock size={10} aria-hidden /> {host}</div>
      <div className="dm-phone__screen" style={{ background: brand.surface, color: brand.ink }}>
        <div className="dm-phone__top">
          {brand.logo ? <img src={brand.logo} alt="" /> : <span className="dm-browser__mark" style={{ background: brand.accent }}>{name.slice(0, 1).toUpperCase()}</span>}
          <span style={title}>{name}</span>
        </div>
        <div className="dm-phone__card">
          <span className="dm-phone__step" style={{ color: brand.accent }}>{t.t("Step 2 of 8")}</span>
          <strong style={title}>{t.t("The clock tower")}</strong>
          <p>{t.t("Count the bells above the door. How many are there?")}</p>
          <span className="dm-phone__input" />
          <span className="dm-phone__btn" style={{ background: brand.accent }}>{t.t("Answer")}</span>
        </div>
      </div>
    </figure>
  );
}

function StorePreview({ host, name, brand, live, ghost }: { host: string; name: string; brand: Brand; live?: boolean; ghost?: boolean }) {
  const t = useT();
  return (
    <figure className={"dm-browser" + (ghost ? " is-ghost" : "")} aria-label={t.t("Preview of your store on {{host}}", { host })}>
      <div className="dm-browser__bar">
        <span className="dm-browser__dots" aria-hidden><i /><i /><i /></span>
        <span className="dm-browser__url">
          <Lock size={11} aria-hidden className={live ? "is-live" : ""} />
          <span>{host}</span>
        </span>
      </div>
      <div className="dm-browser__page" style={{ background: brand.surface, color: brand.ink }}>
        <div className="dm-browser__top">
          {brand.logo ? <img src={brand.logo} alt="" /> : <span className="dm-browser__mark" style={{ background: brand.accent }}>{name.slice(0, 1).toUpperCase()}</span>}
          <span style={brand.font ? { fontFamily: `"${brand.font}", var(--dz-font-sans)` } : undefined}>{name}</span>
        </div>
        <div className="dm-browser__hero" style={{ background: `linear-gradient(135deg, ${brand.accent}, ${brand.accent2})` }}>
          <strong style={brand.font ? { fontFamily: `"${brand.font}", Georgia, serif` } : undefined}>{name}</strong>
          <span>{brand.tagline || t.t("Adventures in your city")}</span>
          <em style={{ color: brand.accent }}>{t.t("Book now")}</em>
        </div>
        <div className="dm-browser__cards">
          {[0, 1].map((i) => (
            <div key={i} className="dm-browser__card">
              <div style={{ background: i ? brand.accent2 : brand.accent }} />
              <i style={{ background: brand.ink }} />
              <i style={{ background: brand.ink }} />
            </div>
          ))}
        </div>
      </div>
    </figure>
  );
}
