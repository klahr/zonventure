import { useEffect, useState } from "react";
import { Lock } from "lucide-react";
import { FONTS, loadFont } from "@zonventure/play-ui";
import { emails } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { EmailPreview, useLoad } from "../../ui/index.ts";
import { storeHeroBackground } from "./brandColors.ts";
import type { OrgProfile } from "./organization-api.ts";
import "./domain.css";

/**
 * An organisation's look as players meet it — its store, a ticket email
 * and a player's screen — and the title typefaces it picks from. Shared
 * by Settings → Organization, where the look is edited, and Settings →
 * Domain, which shows it on the organisation's own address.
 */

/** What the preview shows: the organisation's colours and logo. */
export interface Brand {
  accent: string;
  accent2: string;
  surface: string;
  ink: string;
  logo: string;
  /** The organisation's cover, "" without one. */
  cover: string;
  tagline: string;
  /** The title typeface's key and family, "" for the site's. */
  fontKey: string;
  font: string;
  /** The name it goes by, "" for its own. */
  name: string;
}

export const DEFAULT_BRAND = { accent: "#ad1357", accent2: "#e24080", surface: "#fbfbfd", ink: "#1f3349" };
const HEX = /^#[0-9a-fA-F]{6}$/;

/** The look of profile p; logo and cover are the live ones unless given. */
export function brandOf(p: Partial<OrgProfile> | undefined, orgID: number, opts: { name?: string; logo?: string; cover?: string } = {}): Brand {
  const pick = (v: string | undefined, def: string) => (v && HEX.test(v) ? v : def);
  const key = p?.title_font && FONTS[p.title_font] ? p.title_font : "";
  return {
    accent: pick(p?.accent_color, DEFAULT_BRAND.accent),
    accent2: pick(p?.accent_secondary_color, DEFAULT_BRAND.accent2),
    surface: pick(p?.background_color, DEFAULT_BRAND.surface),
    ink: pick(p?.text_color, DEFAULT_BRAND.ink),
    logo: opts.logo ?? (p?.has_logo && orgID ? `/org-logo/${orgID}?v=${p.accent_color ?? ""}` : ""),
    cover: opts.cover ?? (p?.has_cover && orgID ? `/org-cover/${orgID}` : ""),
    tagline: p?.tagline ?? "",
    fontKey: key,
    font: key ? FONTS[key]! : "",
    name: opts.name ?? "",
  };
}

/** Fonts for a title: those the player ships, by key. */
export const FONT_KEYS = Object.keys(FONTS).sort((a, b) => FONTS[a]!.localeCompare(FONTS[b]!));

/** The title typeface: the site's own (Aclonica) or one the player ships. */
export function FontPicker({ value, onChange, disabled }: { value: string; onChange: (key: string) => void; disabled?: boolean }) {
  const t = useT();
  useEffect(() => {
    for (const k of FONT_KEYS) loadFont(k);
  }, []);
  return (
    <div className="dm-fonts" role="radiogroup" aria-label={t.t("Title typeface")}>
      <button type="button" role="radio" aria-checked={!value} className={"dm-font" + (!value ? " is-on" : "")} disabled={disabled} title={t.t("The site's own typeface, used when you pick none")} onClick={() => onChange("")}>
        <span className="dm-font__sample" style={{ fontFamily: "var(--font-brand)" }}>Aa</span>
        <span className="dm-font__name">Aclonica</span>
      </button>
      {FONT_KEYS.map((k) => (
        <button type="button" role="radio" aria-checked={value === k} key={k} className={"dm-font" + (value === k ? " is-on" : "")} disabled={disabled} onClick={() => onChange(k)}>
          <span className="dm-font__sample" style={{ fontFamily: `"${FONTS[k]}", var(--dz-font-sans)` }}>Aa</span>
          <span className="dm-font__name">{FONTS[k]}</span>
        </button>
      ))}
    </div>
  );
}

/** A title's typeface: the chosen one, else the site's brand face (Aclonica), as the store's headings fall back. */
function titleFont(brand: Brand): string {
  return brand.font ? `"${brand.font}", var(--font-brand)` : "var(--font-brand)";
}

/** The store, a ticket email and a player's screen, as they will look. */
export function Previews(props: { host: string; name: string; brand: Brand; live?: boolean; ghost?: boolean; version?: string }) {
  const t = useT();
  useEffect(() => {
    if (props.brand.fontKey) loadFont(props.brand.fontKey);
  }, [props.brand.fontKey]);
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
      {tab === "email" && <TicketEmailPreview name={props.name} live={!!props.live} version={props.version ?? ""} />}
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
function TicketEmailPreview({ name, live, version }: { name: string; live: boolean; version: string }) {
  const t = useT();
  // version changes once a change to the look is saved: the server renders it.
  const load = useLoad((signal) => emails.preview("team_link", { lang: t.current(), signal }), [t.current(), name, live, version]);
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
  const title = { fontFamily: titleFont(brand) };
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

export function StorePreview({ host, name, brand, live, ghost }: { host: string; name: string; brand: Brand; live?: boolean; ghost?: boolean }) {
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
        <div className="dm-browser__hero" style={{ background: storeHeroBackground(brand.cover, brand.accent, brand.accent2) }}>
          <div className="dm-browser__top">
            <span style={{ fontFamily: titleFont(brand) }}>{name}</span>
            <span className="dm-browser__menu" aria-hidden><i /><i /><i /></span>
          </div>
          {brand.logo ? <span className="dm-browser__logo"><img src={brand.logo} alt="" /></span> : null}
          <span className="dm-browser__chip"><i style={{ background: brand.accent2 }} />{t.t("2 adventures")}</span>
          <strong style={{ fontFamily: titleFont(brand) }}>{name}</strong>
          {brand.tagline && <span className="dm-browser__tagline">{brand.tagline}</span>}
          <span className="dm-browser__ctas">
            <em style={{ background: `linear-gradient(135deg, ${brand.accent}, color-mix(in srgb, ${brand.accent} 70%, ${brand.accent2}))` }}>{t.t("See the adventures")}</em>
            <em className="is-glass">{t.t("I have a code")}</em>
          </span>
        </div>
        <div className="dm-browser__body">
          <span className="dm-browser__eyebrow" style={{ color: brand.accent }}>{t.t("Available now")}</span>
          <b style={{ fontFamily: titleFont(brand) }}>{t.t("Pick an adventure")}</b>
          <div className="dm-browser__cards">
            {[0, 1].map((i) => (
              <div key={i} className="dm-browser__card">
                <div style={{ background: `linear-gradient(135deg, ${brand.accent}, ${brand.accent2})` }}><span className="dm-browser__price" /></div>
                <i style={{ background: brand.ink }} />
                <i style={{ background: brand.ink }} />
              </div>
            ))}
          </div>
        </div>
      </div>
    </figure>
  );
}
