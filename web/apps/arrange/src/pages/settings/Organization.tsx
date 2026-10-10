import { useEffect, useRef, useState } from "react";
import { Copy, ExternalLink } from "lucide-react";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Field, HelpPopover, ICON, ImageDrop, Loading, ProposalPanel, Row, errorText, getProposal, proposedText, useDialogs, useLoad, useToast, type Proposal } from "../../ui/index.ts";
import { deleteImage, getProfile, putProfile, putSlug, uploadImage, type OrgProfile, type ProfileEdit } from "./organization-api.ts";
import { EmailLookCard } from "./organization-email.tsx";
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

function rgba(hex: string, a: number): string {
  const v = fullHex(hex.trim());
  if (!/^#[0-9a-fA-F]{6}$/.test(v)) return `rgba(0, 0, 0, ${a})`;
  return `rgba(${parseInt(v.slice(1, 3), 16)}, ${parseInt(v.slice(3, 5), 16)}, ${parseInt(v.slice(5, 7), 16)}, ${a})`;
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
  };
}

/**
 * The org's public face on /o/{slug} and its store: address, logo,
 * cover, colours and copy, with a live preview of the landing hero.
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
  const toast = useToast();
  const user = state.status === "authenticated" ? state.user : null;
  const orgID = user?.org_id ?? 0;
  const orgName = user?.memberships.find((m) => m.org_id === orgID)?.org_name || t.t("Your organization");

  const [edit, setEdit] = useState<ProfileEdit | null>(null);
  const [slug, setSlug] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [chip, setChip] = useState<string | null>(null);
  const [imgVersion, setImgVersion] = useState(() => Date.now());
  const chipTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const colorTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
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
  }, [profile, propReady, prop.data]);
  useEffect(() => () => { clearTimeout(chipTimer.current); clearTimeout(colorTimer.current); }, []);

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
          public_website: body.public_website.trim(),
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
  const nextSlug = slug.trim().toLowerCase();

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
      setErr(t.t("Slug cannot be empty."));
      return;
    }
    if (nextSlug === profile.slug) return;
    const ok = await dialogs.confirm({
      title: t.t("Rename"),
      message: t.t("Rename the public store URL? Any previously-shared link to /o/{{old}}/store will stop working immediately.", { old: profile.slug }),
      confirmLabel: t.t("Rename"),
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    setErr("");
    try {
      await putSlug(nextSlug);
      const p = await getProfile();
      load.set(p);
      setSlug(p.slug);
    } catch (e) {
      setErr(errorText(e));
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

  const color = (k: ColorKey) => (HEX.test(edit[k].trim()) ? fullHex(edit[k].trim()) : DEFAULTS[k]);
  const accent = color("accent_color");
  const accent2 = color("accent_secondary_color");
  // A held-back picture shows as it would be; it is public once approved.
  const logoSrc = prop.data?.has_image ? `${PROPOSAL}/image?t=${imgVersion}` : profile.has_logo && orgID ? `/org-logo/${orgID}?t=${imgVersion}` : "";
  const coverSrc = prop.data?.has_cover ? `${PROPOSAL}/cover?t=${imgVersion}` : profile.has_cover && orgID ? `/org-cover/${orgID}?t=${imgVersion}` : "";
  const waits = (key: string, live: string) =>
    proposedText(prop.data, key) !== undefined ? t.t("Waits for review. Public now: “{{live}}”", { live: live || t.t("(empty)") }) : undefined;
  const heroBg = coverSrc
    ? `linear-gradient(135deg, ${rgba(accent, 0.6)}, ${rgba(accent2, 0.5)}), url('${coverSrc}') center / cover`
    : `linear-gradient(135deg, ${accent}, ${accent2})`;

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
        className="og-mono"
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
          labels={{ tagline: t.t("Tagline"), public_website: t.t("Website") }}
          pictures={[{ key: "image", label: t.t("Logo") }, { key: "cover", label: t.t("Cover") }]}
          onChanged={() => { prop.reload(); setImgVersion(Date.now()); }}
        />
        <div className="og-grid">
          <div className="og-controls">
            <section className="og-section">
              <h3>{t.t("Identity")}</h3>
              <p className="og-blurb">{t.t("Where customers find you.")}</p>
              <Row gap="sm">
                <a className="og-mono og-url" href={storeURL} target="_blank" rel="noopener">
                  {storeURL}
                  <ExternalLink size={ICON.sm} aria-hidden />
                </a>
                <Button size="sm" variant="ghost" icon={<Copy size={ICON.sm} aria-hidden />} onClick={copyURL}>{t.t("Copy")}</Button>
                {saved("url", t.t("Copied"))}
              </Row>
              <div className="og-slug">
                <Field label={t.t("Slug")}>
                  <input
                    type="text"
                    className="og-mono"
                    spellCheck={false}
                    value={slug}
                    onChange={(e) => setSlug(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void rename(); } }}
                  />
                </Field>
                <Button loading={busy} disabled={nextSlug === profile.slug} onClick={rename}>{t.t("Rename")}</Button>
              </div>
              <p className="og-blurb">{t.t("Renaming breaks every previously-shared /o/old-slug link — share the new one.")}</p>
            </section>

            <section className="og-section">
              <h3>{t.t("Look")}</h3>
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
                {t.t("Brand colours")}
                <HelpPopover label={t.t("Brand colours")} body={t.t("Override the platform palette on your public landing. Leave a field blank to keep the default.")} />
              </div>
              <div className="og-colors">
                {colorRow("accent_color", t.t("Accent"))}
                {colorRow("accent_secondary_color", t.t("Accent (secondary)"))}
                {colorRow("background_color", t.t("Background"))}
                {colorRow("text_color", t.t("Text"))}
              </div>
              {(edit.accent_color || edit.accent_secondary_color || edit.background_color || edit.text_color) && (
                <div>
                  <Button size="sm" variant="ghost" onClick={resetColors}>{t.t("Reset to defaults")}</Button>
                </div>
              )}
            </section>

            <section className="og-section">
              <h3>{t.t("Copy")}</h3>
              <Field label={<>{t.t("Tagline")} {saved("tagline")}</>} hint={[`${edit.tagline.length} / 140`, waits("tagline", profile.tagline)].filter(Boolean).join(" · ")}>
                <input
                  type="text"
                  maxLength={140}
                  placeholder={t.t("A short line about what you offer")}
                  value={edit.tagline}
                  onChange={(e) => setField("tagline", e.target.value)}
                  onBlur={() => void save("tagline")}
                />
              </Field>
              <Field label={<>{t.t("Website")} {saved("website")}</>} {...(waits("public_website", profile.public_website) ? { hint: waits("public_website", profile.public_website) } : {})}>
                <input
                  type="url"
                  placeholder="https://example.com"
                  value={edit.public_website}
                  onChange={(e) => setField("public_website", e.target.value)}
                  onBlur={() => void save("website")}
                />
              </Field>
            </section>
            {err && <p className="og-err" role="alert">{err}</p>}
          </div>

          <div className="og-preview">
            <h3>{t.t("Live preview")}</h3>
            <div className="og-hero" style={{ background: heroBg }}>
              <div className="og-hero__brand">
                <div className="og-hero__logo">{logoSrc ? <img src={logoSrc} alt="" /> : <span>{t.t("Logo")}</span>}</div>
                <div className="og-hero__name">{orgName}</div>
              </div>
              <div className="og-hero__tagline">{edit.tagline.trim() || t.t("Add a tagline below to fill this space.")}</div>
              <div>
                <span className="og-hero__btn" style={{ background: `linear-gradient(135deg, ${accent}, ${accent2})` }}>{t.t("Browse store")}</span>
              </div>
            </div>
            <div className="og-swatches" aria-hidden>
              <span style={{ background: color("background_color"), color: color("text_color") }}>Aa</span>
              <span style={{ background: accent }} />
              <span style={{ background: accent2 }} />
            </div>
            <p className="og-blurb">{t.t("How your public landing hero will look. Changes save automatically.")}</p>
          </div>
        </div>
      </Card>
      <EmailLookCard orgName={orgName} canEdit={user?.role === "org_admin"} />
    </>
  );
}
