import { useEffect, useRef, useState } from "react";
import { ApiError } from "@zonventure/api";
import { Copy, ExternalLink, Palette } from "lucide-react";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Field, HelpPopover, ICON, ImageDrop, Loading, ProposalPanel, Row, errorText, getProposal, proposedText, useDialogs, useLoad, useToast, type Proposal } from "../../ui/index.ts";
import { deleteImage, getProfile, putName, putProfile, putSlug, uploadImage, type OrgProfile, type ProfileEdit } from "./organization-api.ts";
import { EmailLookCard } from "./organization-email.tsx";
import { ShipCountriesCard } from "./organization-shipping.tsx";
import { ContactCard } from "./organization-contact.tsx";
import { colorsFromLogo, contrast, fixContrast } from "./brandColors.ts";
import { FontPicker, Previews, brandOf } from "./brandPreview.tsx";
import { getDomain, getDomainBrand } from "./domain-api.ts";
import "./organization-branding.css";

type ColorKey = "accent_color" | "accent_secondary_color" | "background_color" | "text_color";

const DEFAULTS: Record<ColorKey, string> = {
  accent_color: "#ad1357",
  accent_secondary_color: "#e24080",
  background_color: "#fbfbfd",
  text_color: "#1f3349",
};
const HEX = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;

function fullHex(v: string): string {
  return v.length === 4 ? "#" + v[1] + v[1] + v[2] + v[2] + v[3] + v[3] : v;
}

/** "Nordic Kitchens AB" → "nordic-kitchens-ab": the form internal/orgslug accepts. */
export function slugify(s: string): string {
  return s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

/** The store's address as a browser's bar shows it: "v4.zonventure.com/o/acme"; this site's when the server has no public URL. */
function storeAddress(url: string): string {
  const u = new URL(url, window.location.origin);
  return (u.host + u.pathname).replace(/\/$/, "");
}

/** "example.com" → "https://example.com"; the server takes only full http(s) URLs. */
export function withScheme(url: string): string {
  return url && !/^[a-z][a-z0-9+.-]*:/i.test(url) ? "https://" + url : url;
}

/** The org page's held-back changes (internal/admin/presentation.go). */
const PROPOSAL = "/api/admin/orgs/me/proposal";

function editOf(p: OrgProfile, prop?: Proposal | null): ProfileEdit {
  return {
    tagline: proposedText(prop, "tagline") ?? (p.tagline || ""),
    public_website: proposedText(prop, "public_website") ?? (p.public_website || ""),
    accent_color: (p.accent_color || "").trim(),
    accent_secondary_color: (p.accent_secondary_color || "").trim(),
    background_color: (p.background_color || "").trim(),
    text_color: (p.text_color || "").trim(),
    title_font: p.title_font || "",
  };
}

/**
 * The org's public face on /o/{slug} and its store: address, logo,
 * cover, colours, title typeface and copy, with a live preview of its
 * store, a ticket email and a player's screen. The one place the look
 * is edited; Settings → Domain shows it on the organisation's own
 * address.
 * Copy and colours save on blur (colours 600 ms after the picker
 * stops); the slug has its own confirmed Rename because it breaks
 * every link already shared.
 */
export function OrganizationTab() {
  const t = useT();
  const { state, refresh: refreshSession } = useSession();
  const dialogs = useDialogs();
  const load = useLoad((signal) => getProfile(signal), []);
  const prop = useLoad((signal) => getProposal(PROPOSAL, signal).catch(() => null), []);
  // Its own domain, once live, is the address the preview shows.
  const domain = useLoad((signal) => getDomain(signal).catch(() => null), []);
  const domainBrand = useLoad((signal) => getDomainBrand(signal).catch(() => null), []);
  const toast = useToast();
  const user = state.status === "authenticated" ? state.user : null;
  const orgID = user?.org_id ?? 0;
  const orgName = user?.memberships.find((m) => m.org_id === orgID)?.org_name || t.t("Your organization");

  const [edit, setEdit] = useState<ProfileEdit | null>(null);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState<string | null>(null);
  const [nameErr, setNameErr] = useState("");
  // Under the field, so the autosaves' clearing of err leaves it be.
  const [slugErr, setSlugErr] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [chip, setChip] = useState<string | null>(null);
  const [imgVersion, setImgVersion] = useState(() => Date.now());
  const chipTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const colorTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const textTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const editRef = useRef<ProfileEdit | null>(null);
  editRef.current = edit;
  const saving = useRef<Promise<void> | null>(null);

  const profile = load.data;
  // Waits for the proposal too, so a held-back tagline shows as written.
  const propReady = prop.data !== undefined || !!prop.error;
  useEffect(() => {
    if (!profile || !propReady) return;
    setEdit((e) => e ?? editOf(profile, prop.data));
    setSlug((s) => s || profile.slug);
    setName((n) => n ?? proposedText(prop.data, "name") ?? orgName);
  }, [profile, propReady, prop.data, orgName]);
  useEffect(() => () => { clearTimeout(chipTimer.current); clearTimeout(colorTimer.current); clearTimeout(textTimer.current); }, []);

  const flash = (key: string) => {
    setChip(key);
    clearTimeout(chipTimer.current);
    chipTimer.current = setTimeout(() => setChip(null), 1500);
  };

  // One save at a time; a blur during a save waits for it and then
  // sends the latest values, so no edit is dropped.
  const save = async (chipKey?: string) => {
    while (saving.current) await saving.current;
    const body = editRef.current;
    if (!body) return;
    setErr("");
    const run = (async () => {
      try {
        const p = await putProfile({
          ...body,
          tagline: body.tagline.trim(),
          public_website: withScheme(body.public_website.trim()),
          accent_color: body.accent_color.trim(),
          accent_secondary_color: body.accent_secondary_color.trim(),
          background_color: body.background_color.trim(),
          text_color: body.text_color.trim(),
        });
        if (p) load.set(p);
        prop.reload();
        if (chipKey) flash(chipKey);
      } catch (e) {
        setErr(errorText(e));
      }
    })();
    saving.current = run;
    await run;
    saving.current = null;
  };

  const reread = async () => {
    try {
      const p = await getProfile();
      load.set(p);
      setImgVersion(Date.now());
    } catch (e) {
      setErr(errorText(e));
    }
  };

  if (load.error && !profile) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!profile || !edit) return <Loading label={t.t("Loading…")} />;

  const storeURL = profile.store_url || `/o/${profile.slug}/store`;
  const nextSlug = slugify(slug);

  const copyURL = async () => {
    try {
      await navigator.clipboard.writeText(storeURL);
      flash("url");
    } catch {
      setErr(t.t("Could not copy — select the URL manually."));
    }
  };

  const rename = async () => {
    if (!nextSlug) {
      setSlugErr(t.t("The short name can't be empty."));
      return;
    }
    if (nextSlug === profile.slug) return;
    const ok = await dialogs.confirm({
      title: t.t("Rename"),
      message: t.t("Change your address to /o/{{slug}}? Every link to /o/{{old}} shared so far, the store's included, stops working immediately.", { slug: nextSlug, old: profile.slug }),
      confirmLabel: t.t("Rename"),
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setSlugErr("");
    try {
      await putSlug(nextSlug);
      const p = await getProfile();
      load.set(p);
      setSlug(p.slug);
    } catch (e) {
      setSlugErr(e instanceof ApiError && e.status === 409 ? t.t("Another organization already uses /o/{{slug}}. Try another short name.", { slug: nextSlug }) : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const nextName = (name ?? "").trim();
  const shownName = proposedText(prop.data, "name") ?? orgName;
  const saveName = async () => {
    if (!nextName) {
      setNameErr(t.t("The name can't be empty."));
      return;
    }
    if (nextName === shownName) return;
    setBusy(true);
    setNameErr("");
    try {
      const r = await putName(nextName);
      if (r?.proposed) toast.ok(t.t("Saved — waits for review"));
      else flash("name");
      prop.reload();
      void refreshSession();
    } catch (e) {
      setNameErr(e instanceof ApiError && e.status === 409 ? t.t("Another organization already has that name.") : errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const upload = async (kind: "logo" | "cover", f: File) => {
    setErr("");
    try {
      const r = await uploadImage(kind, f);
      if ((r as { proposed?: boolean } | undefined)?.proposed) toast.ok(t.t("Saved — waits for review"));
      await reread();
      prop.reload();
      if (kind === "logo") void refreshSession();
    } catch (x) {
      setErr(t.t(kind === "logo" ? "Logo upload failed: {{err}}" : "Cover upload failed: {{err}}", { err: errorText(x) }));
    }
  };

  const remove = async (kind: "logo" | "cover") => {
    const ok = await dialogs.confirm({
      title: t.t("Remove"),
      message: kind === "logo" ? t.t("Remove the brand logo?") : t.t("Remove the cover image? The landing page falls back to the accent gradient."),
      confirmLabel: t.t("Remove"),
      danger: true,
    });
    if (!ok) return;
    setErr("");
    try {
      await deleteImage(kind);
      await reread();
      prop.reload();
      if (kind === "logo") void refreshSession();
    } catch (x) {
      setErr(errorText(x));
    }
  };

  const setField = (k: keyof ProfileEdit, v: string) => setEdit((e) => (e ? { ...e, [k]: v } : e));
  // Typing saves after a pause too, not only on blur, so closing the tab doesn't lose it.
  const typeText = (k: "tagline" | "public_website", chipKey: string, v: string) => {
    setField(k, v);
    clearTimeout(textTimer.current);
    textTimer.current = setTimeout(() => void save(chipKey), 800);
  };
  const blurText = (chipKey: string) => {
    clearTimeout(textTimer.current);
    void save(chipKey);
  };
  const pickColor = (k: ColorKey, v: string) => {
    setField(k, v);
    clearTimeout(colorTimer.current);
    colorTimer.current = setTimeout(() => void save(k), 600);
  };
  const resetColors = () => {
    clearTimeout(colorTimer.current);
    setEdit((e) => (e ? { ...e, accent_color: "", accent_secondary_color: "", background_color: "", text_color: "" } : e));
    editRef.current = { ...edit, accent_color: "", accent_secondary_color: "", background_color: "", text_color: "" };
    void save();
  };

  const pickFont = (k: string) => {
    setField("title_font", k);
    editRef.current = { ...edit, title_font: k };
    void save("font");
  };
  // Colours from the logo, or the current ones made readable.
  const applyColors = (c: Pick<ProfileEdit, ColorKey>) => {
    clearTimeout(colorTimer.current);
    setEdit((e) => (e ? { ...e, ...c } : e));
    editRef.current = { ...edit, ...c };
    void save("accent_color");
  };
  const fromLogo = async () => {
    try {
      applyColors(await colorsFromLogo(logoSrc));
    } catch (e) {
      setErr(errorText(e));
    }
  };

  const color = (k: ColorKey) => (HEX.test(edit[k].trim()) ? fullHex(edit[k].trim()) : DEFAULTS[k]);
  const accent = color("accent_color");
  const textOk = contrast(color("text_color"), color("background_color")) >= 4.5;
  const buttonOk = contrast("#ffffff", accent) >= 3;
  const fixColors = () =>
    applyColors({
      accent_color: buttonOk ? accent : fixContrast(accent, "#ffffff", 3),
      accent_secondary_color: color("accent_secondary_color"),
      background_color: color("background_color"),
      text_color: textOk ? color("text_color") : fixContrast(color("text_color"), color("background_color")),
    });
  // A held-back picture shows as it would be; it is public once approved.
  const logoSrc = prop.data?.has_image ? `${PROPOSAL}/image?t=${imgVersion}` : profile.has_logo && orgID ? `/org-logo/${orgID}?t=${imgVersion}` : "";
  const coverSrc = prop.data?.has_cover ? `${PROPOSAL}/cover?t=${imgVersion}` : profile.has_cover && orgID ? `/org-cover/${orgID}?t=${imgVersion}` : "";
  const waits = (key: string, live: string) =>
    proposedText(prop.data, key) !== undefined ? t.t("Waits for review. Public now: “{{live}}”", { live: live || t.t("(empty)") }) : undefined;
  const brand = brandOf(
    { ...profile, ...Object.fromEntries((Object.keys(DEFAULTS) as ColorKey[]).map((k) => [k, color(k)])), tagline: edit.tagline.trim(), title_font: edit.title_font ?? "" },
    orgID,
    { name: shownName, logo: logoSrc, cover: coverSrc },
  );
  const live = domain.data?.domain?.status === "live" ? domain.data.domain : null;
  // On its own domain it goes by the name it chose there.
  const shownAs = (live && domainBrand.data?.display_name) || shownName;

  const saved = (key: string, text = t.t("Saved")) => (
    <span className={"og-chip" + (chip === key ? " is-on" : "")} aria-hidden={chip !== key} role="status">
      {chip === key ? text : ""}
    </span>
  );

  const colorRow = (k: ColorKey, label: string) => (
    <div className="og-color" key={k}>
      <label className="og-color__label" htmlFor={`og-${k}`}>{label}</label>
      <input type="color" aria-label={label} value={color(k)} onChange={(e) => pickColor(k, e.target.value)} />
      <input
        id={`og-${k}`}
        type="text"
        className="cs-mono"
        maxLength={7}
        placeholder={DEFAULTS[k]}
        value={edit[k]}
        onChange={(e) => setField(k, e.target.value)}
        onBlur={() => { clearTimeout(colorTimer.current); void save(k); }}
      />
      {saved(k)}
    </div>
  );

  return (
    <>
      <Card title={t.t("Branding")} subtitle={t.t("How your organization appears to customers: its public page, its store and their address. Everyone in the organization shares these.")}>
        <ProposalPanel
          kind="org"
          base={PROPOSAL}
          proposal={prop.data}
          labels={{ name: t.t("Organization name"), tagline: t.t("Tagline"), public_website: t.t("Website") }}
          pictures={[{ key: "image", label: t.t("Logo") }, { key: "cover", label: t.t("Cover") }]}
          onChanged={() => { prop.reload(); setImgVersion(Date.now()); }}
        />
        <div className="og-grid">
          <div className="og-controls">
            <section className="og-section">
              <h3>{t.t("Identity")}</h3>
              <p className="cs-note">{t.t("Where customers find you.")}</p>
              <div className="og-slug">
                <Field label={<>{t.t("Organization name")} {saved("name")}</>} error={nameErr} hint={waits("name", orgName)}>
                  <input
                    type="text"
                    maxLength={200}
                    value={name ?? ""}
                    onChange={(e) => { setName(e.target.value); setNameErr(""); }}
                    onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void saveName(); } }}
                  />
                </Field>
                <Button loading={busy} disabled={nextName === shownName} onClick={() => void saveName()}>{t.t("Save")}</Button>
              </div>
              <Row gap="sm">
                <a className="cs-mono og-url" href={storeURL} target="_blank" rel="noopener">
                  {storeURL}
                  <ExternalLink size={ICON.sm} aria-hidden />
                </a>
                <Button size="sm" variant="ghost" icon={<Copy size={ICON.sm} aria-hidden />} onClick={copyURL}>{t.t("Copy")}</Button>
                {saved("url", t.t("Copied"))}
              </Row>
              <div className="og-slug">
                <Field label={t.t("Short name in your address")} error={slugErr} hint={nextSlug && nextSlug !== profile.slug ? t.t("Becomes /o/{{slug}} — press Rename to switch.", { slug: nextSlug }) : t.t("The part after /o/. Edit it to rename.")}>
                  <input
                    type="text"
                    className="cs-mono"
                    spellCheck={false}
                    value={slug}
                    onChange={(e) => { setSlug(e.target.value); setSlugErr(""); }}
                    onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void rename(); } }}
                  />
                </Field>
                <Button loading={busy} disabled={nextSlug === profile.slug} onClick={rename}>{t.t("Rename")}</Button>
              </div>
              <p className="cs-note">{t.t("Renaming breaks every link to /o/{{slug}} shared so far — share the new one.", { slug: profile.slug })}</p>
            </section>

            <section className="og-section">
              <h3>{t.t("Branding")}</h3>
              <div className="og-images">
                <div>
                  <div className="og-label">{t.t("Logo")}</div>
                  <ImageDrop
                    shape="logo"
                    src={logoSrc}
                    label={t.t("Upload logo")}
                    title={t.t("Click to upload a logo")}
                    accept="image/png,image/jpeg,image/webp,image/svg+xml"
                    onFile={(f) => void upload("logo", f)}
                    onRemove={() => void remove("logo")}
                  />
                </div>
                <div className="og-images__cover">
                  <div className="og-label">{t.t("Cover")}</div>
                  <ImageDrop
                    shape="cover"
                    src={coverSrc}
                    label={t.t("Upload cover image")}
                    title={t.t("Click to upload a cover")}
                    onFile={(f) => void upload("cover", f)}
                    onRemove={() => void remove("cover")}
                  />
                </div>
              </div>
              <div className="og-label og-label--strong">
                {t.t("Brand colors")}
                <HelpPopover label={t.t("Brand colors")} body={t.t("Override the platform palette on your public landing. Leave a field blank to keep the default.")} />
              </div>
              <div className="og-colors">
                {colorRow("accent_color", t.t("Accent"))}
                {colorRow("accent_secondary_color", t.t("Accent (secondary)"))}
                {colorRow("background_color", t.t("Background"))}
                {colorRow("text_color", t.t("Text"))}
              </div>
              {(!textOk || !buttonOk) && <p className="cs-err" role="status">{t.t("Some text would be hard to read on these colours.")}</p>}
              <Row gap="sm">
                {logoSrc && <Button size="sm" variant="ghost" icon={<Palette size={ICON.sm} aria-hidden />} onClick={() => void fromLogo()}>{t.t("Pick from my logo")}</Button>}
                {(!textOk || !buttonOk) && <Button size="sm" variant="primary" onClick={fixColors}>{t.t("Fix contrast")}</Button>}
                {(edit.accent_color || edit.accent_secondary_color || edit.background_color || edit.text_color) && (
                  <Button size="sm" variant="ghost" onClick={resetColors}>{t.t("Reset to defaults")}</Button>
                )}
              </Row>
              <div className="og-label og-label--strong">{t.t("Title typeface")} {saved("font")}</div>
              <FontPicker value={edit.title_font ?? ""} onChange={pickFont} disabled={user?.role !== "org_admin"} />
            </section>

            <section className="og-section">
              <h3>{t.t("Copy")}</h3>
              <Field label={<>{t.t("Tagline")} {saved("tagline")}</>} hint={[`${edit.tagline.length} / 140`, waits("tagline", profile.tagline)].filter(Boolean).join(" · ")}>
                <input
                  type="text"
                  maxLength={140}
                  placeholder={t.t("A short line about what you offer")}
                  value={edit.tagline}
                  onChange={(e) => typeText("tagline", "tagline", e.target.value)}
                  onBlur={() => blurText("tagline")}
                />
              </Field>
              <Field label={<>{t.t("Website")} {saved("website")}</>} {...(waits("public_website", profile.public_website) ? { hint: waits("public_website", profile.public_website) } : {})}>
                <input
                  type="url"
                  placeholder="https://example.com"
                  value={edit.public_website}
                  onChange={(e) => typeText("public_website", "website", e.target.value)}
                  onBlur={() => {
                    setField("public_website", withScheme(edit.public_website.trim()));
                    blurText("website");
                  }}
                />
              </Field>
            </section>
            {err && <p className="cs-err" role="alert">{err}</p>}
          </div>

          <div className="og-preview">
            <h3>{t.t("Live preview")}</h3>
            <Previews
              host={live ? live.host : storeAddress(storeURL)}
              name={shownAs}
              brand={brand}
              live
              version={[profile.accent_color, profile.accent_secondary_color, profile.background_color, profile.text_color, profile.has_logo, profile.title_font, imgVersion].join("|")}
            />
          </div>
        </div>
      </Card>
      <EmailLookCard
        orgName={orgName}
        canEdit={user?.role === "org_admin"}
        brand={[profile.accent_color, profile.accent_secondary_color, profile.background_color, profile.text_color, profile.has_logo, imgVersion].join("|")}
      />
      <ContactCard canEdit={user?.role === "org_admin"} />
      <ShipCountriesCard canEdit={user?.role === "org_admin"} />
    </>
  );
}
