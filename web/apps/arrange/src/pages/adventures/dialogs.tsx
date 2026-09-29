import { useEffect, useRef, useState } from "react";
import { ImagePlus, Trash2 } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href } from "../../router.ts";
import { Button, Dialog, Field, HelpPopover, ICON, Row, fmtDateTime, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { createAdventure, downscaleImage, getEdit, importAdventure, orgName, removeCover, updateAdventure, uploadCover, type AdventurePatch, type AdventureEdit, type Assignment } from "./api.ts";
import { Cover } from "./parts.tsx";

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
  const badSlug = slug !== "" && !SLUG.test(slug);
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
      <Field label={t.t("Description (optional)")}>
        <textarea rows={2} value={description} placeholder={t.t("A short pitch — what players encounter and roughly how long it takes.")} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <Field label={t.t("Author")}>
        <input value={author} onChange={(e) => setAuthor(e.target.value)} />
      </Field>
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

type Form = { slug: string; title: string; description: string; author: string; language: string; intro_text: string; outro_text: string; strict_assets: boolean; lat: string; lng: string; start_label: string };

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
export function EditDetailsDialog({ slug, accent, firstPlace, onClose, onSaved, onCover }: {
  slug: string;
  accent?: string | undefined;
  /** Where "Use first place" copies the start from: the first gated puzzle or zone. */
  firstPlace: { lat: number; lng: number } | null;
  onClose: () => void;
  onSaved: (slug: string) => void;
  onCover: (url: string) => void;
}) {
  const t = useT();
  const toast = useToast();
  const load = useLoad((signal) => getEdit(slug, signal), [slug]);
  const base = load.data;
  const err = load.error;
  const [f, setF] = useState<Form | null>(null);
  const [busy, setBusy] = useState(false);
  const [cover, setCover] = useState<string | undefined>(undefined);
  const file = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!base) return;
    setF(formOf(base));
    setCover(base.avatar_url);
  }, [base]);

  const initial = base ? formOf(base) : null;
  const dirty = !!(f && initial && (Object.keys(f) as (keyof Form)[]).some((k) => (typeof f[k] === "string" ? (f[k] as string).trim() !== (initial[k] as string).trim() : f[k] !== initial[k])));
  const close = useGuardedClose(dirty, t.t("Discard unsaved changes to this adventure?"), onClose);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((x) => (x ? { ...x, [k]: v } : x));
  const badSlug = !!f && !SLUG.test(f.slug.trim());
  const latBad = !!f && f.lat.trim() !== "" && coord(f.lat) == null;
  const lngBad = !!f && f.lng.trim() !== "" && coord(f.lng) == null;
  const halfStart = !!f && (coord(f.lat) == null) !== (coord(f.lng) == null);

  const save = async () => {
    if (!f || !initial || badSlug || !f.title.trim() || latBad || lngBad || halfStart) return;
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

  const onFile = async () => {
    const picked = file.current?.files?.[0];
    if (file.current) file.current.value = "";
    if (!picked) return;
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
          <Button type="submit" variant="primary" filled loading={busy} disabled={!f || badSlug || !f.title.trim() || latBad || lngBad || halfStart}>{t.t("Save")}</Button>
        </>
      }
    >
      {err && <p className="cs-field__error" role="alert">{err}</p>}
      {!f && !err && <p className="cs-loading">{t.t("Loading…")}</p>}
      {f && (
        <>
          <div className="adv-cover-row">
            <Cover title={f.title} src={cover} size={72} accent={accent} />
            <div className="adv-cover-row__text">
              <span className="cs-field__label">{t.t("Cover picture")}</span>
              <Row gap="sm">
                <Button size="sm" icon={<ImagePlus size={ICON.sm} aria-hidden />} onClick={() => file.current?.click()}>{t.t("Upload cover")}</Button>
                {cover && <Button size="sm" variant="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={dropCover}>{t.t("Remove cover")}</Button>}
              </Row>
              <input ref={file} type="file" accept="image/*" hidden onChange={onFile} aria-label={t.t("Upload cover")} />
            </div>
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
            <textarea rows={5} value={f.intro_text} onChange={(e) => set("intro_text", e.target.value)} />
          </Field>
          <Field label={t.t("Outro — shown on the completion screen")} hint={t.t("Markdown. Leave empty for the default celebratory line.")}>
            <textarea rows={3} value={f.outro_text} onChange={(e) => set("outro_text", e.target.value)} />
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
            {halfStart && <p className="cs-field__error" role="alert">{t.t("Set both coordinates, or clear both.")}</p>}
            <Row gap="sm">
              <Button
                size="sm"
                variant="ghost"
                disabled={!firstPlace}
                title={firstPlace ? undefined : t.t("No gated puzzle to copy from yet.")}
                onClick={() => firstPlace && setF((x) => (x ? { ...x, lat: String(firstPlace.lat), lng: String(firstPlace.lng) } : x))}
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
          <p className="cs-field__hint">{t.t("Colours, fonts and the adventure's language for players are set in the designer's Look.")}</p>
        </>
      )}
    </Dialog>
  );
}
