import { useEffect, useRef, useState } from "react";
import { Eye, EyeOff } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href } from "../../router.ts";
import { Button, Dialog, Field, HelpPopover, ICON, ImageDrop, MarkdownArea, Row, fmtDateTime, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { createAdventure, downscaleImage, firstPlaceOf, getEdit, getGraphState, importAdventure, orgName, removeCover, updateAdventure, uploadCover, type AdventurePatch, type AdventureEdit, type Assignment } from "./api.ts";
import { PointMap } from "../../designer/PointMap.tsx";

const SLUG = /^[a-z0-9-]+$/;

const slugFrom = (s: string) =>
  s.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 60);

/**
 * Close a form dialog, asking first when something was typed: a click
 * on the backdrop or Escape would otherwise lose it. The confirm's own
 * Escape reaches this handler too, so a close is ignored while asking.
 */
function useGuardedClose(dirty: boolean, question: string, onClose: () => void) {
  const t = useT();
  const dialogs = useDialogs();
  const asking = useRef(false);
  return async () => {
    if (asking.current) return;
    if (!dirty) return onClose();
    asking.current = true;
    const ok = await dialogs.confirm({ title: question, confirmLabel: t.t("Discard"), danger: true });
    asking.current = false;
    if (ok) onClose();
  };
}

/** New adventure: title first, the slug follows it until edited. Straight on to the designer. */
export function CreateAdventureDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const org = orgName(state.status === "authenticated" ? state.user : null);
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [description, setDescription] = useState("");
  const [author, setAuthor] = useState(org);
  const [busy, setBusy] = useState(false);
  // The short name and author fill themselves in; they open when they need a hand.
  const [more, setMore] = useState(false);
  const badSlug = slug !== "" && !SLUG.test(slug);
  const moreOpen = more || badSlug || (title.trim() !== "" && slug === "");
  const close = useGuardedClose(!!(title || slug || description || author !== org), t.t("Discard this adventure?"), onClose);

  const submit = async () => {
    if (!title.trim() || !slug.trim() || badSlug) return;
    setBusy(true);
    try {
      const r = await createAdventure({ slug: slug.trim(), title: title.trim(), description, author: author.trim() });
      location.hash = href({ name: "designer", slug: r.slug });
      onClose();
    } catch (e) {
      toast.err(e);
      // A taken short name is the usual reason; show the field.
      setMore(true);
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("New adventure")}
      onClose={close}
      onSubmit={submit}
      footer={
        <>
          <Button onClick={close}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" filled loading={busy} disabled={!title.trim() || !slug.trim() || badSlug}>{t.t("Create and design")}</Button>
        </>
      }
    >
      <Field label={t.t("Title")}>
        <input
          autoFocus
          required
          value={title}
          placeholder={t.t("e.g. “The Lost Vault”")}
          onChange={(e) => {
            setTitle(e.target.value);
            if (!slugEdited) setSlug(slugFrom(e.target.value));
          }}
        />
      </Field>
      <Field label={t.t("Description (optional)")}>
        <textarea rows={2} value={description} placeholder={t.t("A short pitch — what players encounter and roughly how long it takes.")} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <details className="adv-more" open={moreOpen} onToggle={(e) => setMore((e.target as HTMLDetailsElement).open)}>
        <summary>{t.t("Link name and author")}</summary>
      <Field label={t.t("Short name")} error={badSlug ? t.t("Lowercase a–z, digits, and hyphens only.") : undefined} hint={t.t("Used in links. Lowercase a–z, digits and hyphens.")}>
        <input
          required
          value={slug}
          placeholder="the-lost-vault"
          aria-invalid={badSlug || undefined}
          onChange={(e) => {
            setSlug(e.target.value);
            setSlugEdited(e.target.value !== "");
          }}
        />
      </Field>
      <Field label={t.t("Author")}>
        <input value={author} onChange={(e) => setAuthor(e.target.value)} />
      </Field>
      </details>
      <p className="cs-field__hint">{t.t("Next you design its steps. Colours, pictures and the rest can be set any time.")}</p>
    </Dialog>
  );
}

/**
 * Import a .zvadv.zip. A taken slug (409) — the usual case when
 * re-importing — asks for another instead of making the author pick the
 * file again.
 */
export function useImportAdventure() {
  const t = useT();
  const toast = useToast();
  const [retry, setRetry] = useState<File | null>(null);

  const done = (r: { slug: string; title?: string }) => {
    toast.ok(t.t('Imported "{{title}}".', { title: r.title || r.slug }));
    location.hash = href({ name: "adventure", slug: r.slug });
  };

  const upload = async (file: File, slug?: string) => {
    try {
      done(await importAdventure(file, slug));
      setRetry(null);
    } catch (e) {
      if (e instanceof ApiError && (e.status === 409 || e.status === 500) && !slug) setRetry(file);
      else toast.err(e);
    }
  };

  const pick = () => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".zip,application/zip";
    input.addEventListener("change", () => {
      const f = input.files?.[0];
      if (f) void upload(f);
    });
    input.click();
  };

  const dialog = retry ? <RenameImportDialog onClose={() => setRetry(null)} onSubmit={(slug) => upload(retry, slug)} /> : null;
  return { pick, dialog };
}

function RenameImportDialog({ onClose, onSubmit }: { onClose: () => void; onSubmit: (slug: string) => Promise<void> }) {
  const t = useT();
  const [slug, setSlug] = useState("");
  const [busy, setBusy] = useState(false);
  const bad = slug !== "" && !SLUG.test(slug.trim());
  return (
    <Dialog
      title={t.t("Import")}
      onClose={onClose}
      onSubmit={async () => {
        if (!slug.trim() || bad) return;
        setBusy(true);
        await onSubmit(slug.trim());
        setBusy(false);
      }}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" filled loading={busy} disabled={!slug.trim() || bad}>{t.t("Import")}</Button>
        </>
      }
    >
      <Field label={t.t("That slug already exists or could not be created. Pick a new slug:")} error={bad ? t.t("Lowercase a–z, digits, and hyphens only.") : undefined}>
        <input autoFocus value={slug} onChange={(e) => setSlug(e.target.value)} placeholder="the-lost-vault" />
      </Field>
    </Dialog>
  );
}

/** Several event passes for one adventure: say which one gets spent. */
export function PickPassDialog({ title, passes, onPick, onClose }: { title: string; passes: Assignment[]; onPick: (id: number) => void; onClose: () => void }) {
  const t = useT();
  return (
    <Dialog title={t.t("Pick an event pass")} onClose={onClose} footer={<Button onClick={onClose}>{t.t("Cancel")}</Button>}>
      <p>{t.t("You hold several event passes for {{title}}. Which one do you want to use?", { title })}</p>
      <div className="adv-choices">
        {passes.map((a, i) => (
          <Button key={a.id} autoFocus={i === 0} className="adv-choice" onClick={() => onPick(a.id)}>
            {t.t("up to {{n}} players · purchased {{when}}", { n: a.max_players, when: fmtDateTime(a.created_at, t.current()) })}
          </Button>
        ))}
      </div>
    </Dialog>
  );
}

const LANGS = ["en", "sv", "no", "da", "fi", "de", "fr", "es", "pt", "pt-BR", "it", "nl", "pl", "ja", "zh-Hant", "zh-Hans"];

type Form = { slug: string; title: string; description: string; author: string; language: string; intro_text: string; outro_text: string; strict_assets: boolean; lat: string; lng: string; start_label: string; require_start: boolean; radius: string; cheat_password: string };

function formOf(a: AdventureEdit): Form {
  return {
    slug: a.slug,
    title: a.title,
    description: a.description || "",
    author: a.author || "",
    language: a.language || "en",
    intro_text: a.intro_text || "",
    outro_text: a.outro_text || "",
    strict_assets: !!a.strict_assets,
    lat: a.start_lat == null ? "" : String(a.start_lat),
    lng: a.start_lng == null ? "" : String(a.start_lng),
    start_label: a.start_label || "",
    require_start: a.start_radius_m != null,
    radius: a.start_radius_m == null ? "50" : String(a.start_radius_m),
    cheat_password: a.cheat_password || "",
  };
}

const coord = (s: string) => {
  const v = s.trim();
  if (v === "") return null;
  const n = Number.parseFloat(v);
  return Number.isFinite(n) ? n : null;
};

/**
 * The adventure's own details: names, pitch, language, the welcome and
 * completion copy, where teams meet, strict assets, and the cover. The
 * palette and fonts are the designer's (Look). Only changed fields are
 * sent; a cleared start coordinate is sent as null, which clears it.
 */
export function EditDetailsDialog({ slug, firstPlace, onClose, onSaved, onCover }: {
  slug: string;
  /** Where "Use first place" copies the start from: the first gated puzzle or zone. */
  firstPlace: { lat: number; lng: number } | null;
  onClose: () => void;
  onSaved: (slug: string) => void;
  onCover: (url: string) => void;
}) {
  const t = useT();
  const toast = useToast();
  const load = useLoad((signal) => getEdit(slug, signal), [slug]);
  // Nothing published to copy a start from: the draft's first place instead.
  const draft = useLoad((signal) => (firstPlace ? Promise.resolve(null) : getGraphState(slug, signal).catch(() => null)), [slug, !!firstPlace]);
  const place = firstPlace ?? firstPlaceOf(draft.data?.envelope?.graph);
  const base = load.data;
  const err = load.error;
  const [f, setF] = useState<Form | null>(null);
  const [busy, setBusy] = useState(false);
  const [cover, setCover] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!base) return;
    setF(formOf(base));
    setCover(base.avatar_url);
  }, [base]);

  const initial = base ? formOf(base) : null;
  const dirty = !!(f && initial && (Object.keys(f) as (keyof Form)[]).some((k) => (typeof f[k] === "string" ? (f[k] as string).trim() !== (initial[k] as string).trim() : f[k] !== initial[k])));
  const close = useGuardedClose(dirty, t.t("Discard unsaved changes to this adventure?"), onClose);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((x) => (x ? { ...x, [k]: v } : x));
  // Hidden until asked for, so it isn't shown to whoever is looking at the screen.
  const [showCheat, setShowCheat] = useState(false);
  const badSlug = !!f && !SLUG.test(f.slug.trim());
  const latBad = !!f && f.lat.trim() !== "" && coord(f.lat) == null;
  const lngBad = !!f && f.lng.trim() !== "" && coord(f.lng) == null;
  const halfStart = !!f && (coord(f.lat) == null) !== (coord(f.lng) == null);
  // A required start needs a point and a radius within what the server accepts.
  const radiusOf = (x: Form) => (x.require_start ? coord(x.radius) : null);
  const radiusBad = !!f && f.require_start && (radiusOf(f) == null || radiusOf(f)! < 5 || radiusOf(f)! > 5000);
  const requireWithoutPoint = !!f && f.require_start && (coord(f.lat) == null || coord(f.lng) == null);

  const save = async () => {
    if (!f || !initial || badSlug || !f.title.trim() || latBad || lngBad || halfStart || radiusBad || requireWithoutPoint) return;
    const p: AdventurePatch = {};
    if (f.slug.trim() !== initial.slug) p.slug = f.slug.trim();
    if (f.title.trim() !== initial.title) p.title = f.title.trim();
    if (f.description !== initial.description) p.description = f.description;
    if (f.author.trim() !== initial.author) p.author = f.author.trim();
    if (f.language.trim() !== initial.language) p.language = f.language.trim();
    if (f.intro_text !== initial.intro_text) p.intro_text = f.intro_text;
    if (f.outro_text !== initial.outro_text) p.outro_text = f.outro_text;
    if (f.strict_assets !== initial.strict_assets) p.strict_assets = f.strict_assets;
    if (coord(f.lat) !== coord(initial.lat)) p.start_lat = coord(f.lat);
    if (coord(f.lng) !== coord(initial.lng)) p.start_lng = coord(f.lng);
    if (f.start_label.trim() !== initial.start_label) p.start_label = f.start_label.trim();
    if (radiusOf(f) !== radiusOf(initial)) p.start_radius_m = radiusOf(f);
    if (f.cheat_password !== initial.cheat_password) p.cheat_password = f.cheat_password;
    if (Object.keys(p).length === 0) return onClose();
    setBusy(true);
    try {
      await updateAdventure(slug, p);
      toast.ok(t.t("Saved"));
      onSaved(p.slug ?? slug);
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };

  const onFile = async (picked: File) => {
    try {
      const r = await uploadCover(slug, await downscaleImage(picked));
      const url = (r.url || `/adventure-assets/avatar/${encodeURIComponent(slug)}`) + "?v=" + Date.now();
      setCover(url);
      onCover(url);
      toast.ok(t.t("Cover updated"));
    } catch (e) {
      toast.err(e);
    }
  };

  const dropCover = async () => {
    try {
      await removeCover(slug);
      setCover(undefined);
      onCover("");
      toast.ok(t.t("Cover removed"));
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <Dialog
      wide
      title={t.t("Edit adventure")}
      onClose={close}
      onSubmit={save}
      footer={
        <>
          <Button onClick={close}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" filled loading={busy} disabled={!f || badSlug || !f.title.trim() || latBad || lngBad || halfStart || radiusBad || requireWithoutPoint}>{t.t("Save")}</Button>
        </>
      }
    >
      {err && <p className="cs-field__error" role="alert">{err}</p>}
      {!f && !err && <p className="cs-loading">{t.t("Loading…")}</p>}
      {f && (
        <>
          <div className="adv-cover-row__text">
            <span className="cs-field__label">{t.t("Cover picture")}</span>
            <ImageDrop
              shape="photo"
              src={cover}
              label={t.t("Upload cover")}
              title={t.t("Click to upload a cover")}
              accept="image/*"
              onFile={(f) => void onFile(f)}
              onRemove={() => void dropCover()}
              removeLabel={t.t("Remove cover")}
            />
          </div>
          <Field label={t.t("Title")}>
            <input autoFocus required value={f.title} onChange={(e) => set("title", e.target.value)} />
          </Field>
          <Field label={t.t("Short name")} error={badSlug ? t.t("Lowercase a–z, digits, and hyphens only.") : undefined} hint={t.t("Used in links. Renaming breaks links already shared.")}>
            <input required value={f.slug} onChange={(e) => set("slug", e.target.value)} aria-invalid={badSlug || undefined} />
          </Field>
          <Field label={t.t("Description")}>
            <textarea rows={2} value={f.description} onChange={(e) => set("description", e.target.value)} />
          </Field>
          <div className="adv-form-grid">
            <Field label={t.t("Author")}>
              <input value={f.author} onChange={(e) => set("author", e.target.value)} />
            </Field>
            <Field label={t.t("Language")} hint={t.t("BCP 47 tag — surfaced on the Catalog so buyers can spot adventures in their language.")}>
              <input value={f.language} list="adv-lang-suggestions" maxLength={32} placeholder="en" onChange={(e) => set("language", e.target.value)} />
            </Field>
          </div>
          <datalist id="adv-lang-suggestions">
            {LANGS.map((c) => <option key={c} value={c} />)}
          </datalist>
          <Field label={t.t("Intro — shown on the welcome screen")} hint={t.t("Markdown. Leave empty for the default welcome + tips.")}>
            <MarkdownArea rows={5} value={f.intro_text} onChange={(e) => set("intro_text", e.target.value)} />
          </Field>
          <Field label={t.t("Outro — shown on the completion screen")} hint={t.t("Markdown. Leave empty for the default celebratory line.")}>
            <MarkdownArea rows={3} value={f.outro_text} onChange={(e) => set("outro_text", e.target.value)} />
          </Field>
          <fieldset className="adv-fieldset">
            <legend className="cs-field__label">
              {t.t("Starting location (optional)")}{" "}
              <HelpPopover label={t.t("About the starting location")} body={t.t("Shown on the catalog page and the player welcome overlay so the team knows where to meet up. Leave blank to fall back to the first gated puzzle's location.")} />
            </legend>
            <div className="adv-form-grid">
              <Field label={t.t("Lat")} error={latBad ? t.t("Not a number.") : undefined}>
                <input type="number" step="any" min={-90} max={90} inputMode="decimal" placeholder="59.3214" value={f.lat} onChange={(e) => set("lat", e.target.value)} />
              </Field>
              <Field label={t.t("Lng")} error={lngBad ? t.t("Not a number.") : undefined}>
                <input type="number" step="any" min={-180} max={180} inputMode="decimal" placeholder="18.0641" value={f.lng} onChange={(e) => set("lng", e.target.value)} />
              </Field>
            </div>
            <Field label={t.t("Label")}>
              <input maxLength={200} value={f.start_label} placeholder={t.t("e.g. “Monteliusvägen, vid bänken under trappan”")} onChange={(e) => set("start_label", e.target.value)} />
            </Field>
            <label className="adv-check">
              <input type="checkbox" checked={f.require_start} onChange={(e) => set("require_start", e.target.checked)} />
              <span>{t.t("Teams must be at the starting point to begin")}</span>
              <HelpPopover label={t.t("Teams must be at the starting point to begin")} body={t.t("The start stays locked on the phones until a player on the team is within this distance of the starting point. The clock starts when the team begins, not when the event does.")} />
            </label>
            {f.require_start && (
              <Field label={t.t("Radius (metres)")} error={radiusBad ? t.t("Between 5 and 5000 metres.") : undefined}>
                <input type="number" min={5} max={5000} step={5} inputMode="numeric" value={f.radius} onChange={(e) => set("radius", e.target.value)} />
              </Field>
            )}
            {requireWithoutPoint && <p className="cs-field__error" role="alert">{t.t("Set the starting point first.")}</p>}
            {halfStart && <p className="cs-field__error" role="alert">{t.t("Set both coordinates, or clear both.")}</p>}
            <PointMap
              value={coord(f.lat) != null && coord(f.lng) != null ? { lat: coord(f.lat)!, lng: coord(f.lng)! } : null}
              fallback={place}
              onChange={(p) => setF((x) => (x ? { ...x, lat: String(p.lat), lng: String(p.lng) } : x))}
            />
            <Row gap="sm">
              <Button
                size="sm"
                variant="ghost"
                disabled={!place}
                title={place ? undefined : t.t("No gated puzzle to copy from yet.")}
                onClick={() => place && setF((x) => (x ? { ...x, lat: String(place.lat), lng: String(place.lng) } : x))}
              >
                {t.t("Use first gated puzzle")}
              </Button>
            </Row>
          </fieldset>
          <label className="adv-check">
            <input type="checkbox" checked={f.strict_assets} onChange={(e) => set("strict_assets", e.target.checked)} />
            <span>
              <strong>{t.t("Strict assets")}</strong> — {t.t("Hide images and audio for pages the team hasn't reached yet")}{" "}
              <HelpPopover label={t.t("About strict assets")} body={t.t("Default off lets the offline cache prefetch every page's media. Turn on for hunts where the picture is the puzzle (find-the-glyph, hidden QR, map fragments).")} />
            </span>
          </label>
          <Field
            label={t.t("Cheat password")}
            hint={t.t("Optional, for testing. Leave empty for none. On the phone, tap the adventure's title 7 times and enter it: every answer then counts as right, for no points, and the team is left out of the rankings.")}
          >
            <Row>
              <input
                type={showCheat ? "text" : "password"}
                autoComplete="off"
                value={f.cheat_password}
                onChange={(e) => set("cheat_password", e.target.value)}
              />
              <Button
                variant="ghost"
                size="icon"
                aria-label={showCheat ? t.t("Hide password") : t.t("Show password")}
                title={showCheat ? t.t("Hide password") : t.t("Show password")}
                aria-pressed={showCheat}
                icon={showCheat ? <EyeOff size={ICON.sm} aria-hidden /> : <Eye size={ICON.sm} aria-hidden />}
                onClick={() => setShowCheat((v) => !v)}
              />
            </Row>
          </Field>
          <p className="cs-field__hint">{t.t("Colours, fonts and the adventure's language for players are set in the designer's Look.")}</p>
        </>
      )}
    </Dialog>
  );
}
