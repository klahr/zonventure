import { useMemo, useState } from "react";
import { ApiError, adventures, type play } from "@zonventure/api";
import { FORMAT_V2, type Graph, type ThemeLook } from "@zonventure/graph";
import { DEFAULT_LOOK, PlayView, STARTER_THEMES, type Feedback } from "@zonventure/play-ui";
import "@zonventure/play-ui/play-ui.css";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href } from "../../router.ts";
import { Button, Dialog, Field, useToast } from "../../ui/index.ts";
import { PointMap } from "../../designer/PointMap.tsx";
import { PhoneFrame } from "../../designer/SamplePreview.tsx";
import { StarterGallery } from "../../designer/StarterGallery.tsx";
import { saveTutorial } from "../../designer/tutorial.ts";
import { createAdventure, orgName, updateAdventure } from "./api.ts";
import { slugFrom, useGuardedClose } from "./dialogs.tsx";
import "./new-adventure.css";

const SLUG = /^[a-z0-9-]+$/;

/** The languages offered first; the designer's settings take any other. */
const LANGUAGES: { code: string; name: string }[] = [
  { code: "sv", name: "Svenska" },
  { code: "en", name: "English" },
  { code: "no", name: "Norsk" },
  { code: "da", name: "Dansk" },
  { code: "fi", name: "Suomi" },
  { code: "de", name: "Deutsch" },
  { code: "fr", name: "Français" },
  { code: "es", name: "Español" },
];

const AUDIENCES = [
  { value: "kids", label: "Kids" },
  { value: "families", label: "Families" },
  { value: "adults", label: "Adults" },
] as const;

type Step = 0 | 1 | 2 | 3;

/**
 * A new adventure, step by step: what it is called and who it is for,
 * how it looks, where it starts, then a look at it all before it is
 * made. A phone beside the steps shows the adventure's first page as it
 * takes shape. Made, it opens in the designer with the chosen look as
 * its main look and its first saved theme, and a first step to build on.
 *
 * For the tutorial it comes filled in with its example, Erik's stolen
 * elephants in the jungle, and the designer opens with the guide.
 */
export default function NewAdventure({ onClose, tutorial = false }: { onClose: () => void; tutorial?: boolean }) {
  const t = useT();
  const [example] = useState(() => (tutorial ? exampleOf(t) : null));
  const toast = useToast();
  const { state } = useSession();
  const [step, setStep] = useState<Step>(0);
  const [title, setTitle] = useState(example?.title ?? "");
  const [slug, setSlug] = useState(example ? slugFrom(example.title) : "");
  const [slugEdited, setSlugEdited] = useState(false);
  const [slugError, setSlugError] = useState<string | null>(null);
  const [pitch, setPitch] = useState(example?.pitch ?? "");
  const [author] = useState(orgName(state.status === "authenticated" ? state.user : null));
  const [language, setLanguage] = useState(() => (LANGUAGES.some((l) => l.code === t.current()) ? t.current() : "en"));
  const [audience, setAudience] = useState<(typeof AUDIENCES)[number]["value"] | "">(example ? "families" : "");
  const [theme, setTheme] = useState<{ id: string; name: string; look: ThemeLook }>(example?.theme ?? { id: "default", name: "", look: DEFAULT_LOOK as ThemeLook });
  const [start, setStart] = useState<{ lat: number; lng: number } | null>(null);
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const [busy, setBusy] = useState(false);
  const close = useGuardedClose(!!(title || pitch) && !example, t.t("Discard this adventure?"), onClose);

  const badSlug = slug !== "" && !SLUG.test(slug);
  const basicsDone = title.trim() !== "" && slug.trim() !== "" && !badSlug;
  const steps = [t.t("The basics"), t.t("The look"), t.t("Where it starts"), t.t("Ready")];
  const shownTitle = title.trim() || t.t("Your adventure");
  const welcome = pitch.trim() || t.t("Welcome! Your first step: tell the teams what this adventure is about.");

  // The adventure's first page, as a team will see it, in the chosen look.
  const view = useMemo(
    () =>
      ({
        node: "welcome",
        type: "story",
        title: shownTitle,
        blocks: [
          { id: "title", kind: "title", text: shownTitle },
          { id: "b1", kind: "text", text: welcome },
        ],
        score: 0,
        penalty_score: 0,
        now: new Date().toISOString(),
      }) as unknown as play.View,
    [shownTitle, welcome],
  );

  const create = async () => {
    if (!basicsDone || busy) return;
    setBusy(true);
    let made: string;
    try {
      made = await createFree(slug.trim(), !!example && !slugEdited, (s) => createAdventure({ slug: s, title: title.trim(), description: pitch.trim(), author }));
    } catch (e) {
      // A taken short name is the usual reason: back to where it is set.
      setSlugError(e instanceof Error ? e.message : t.t("Could not create the adventure."));
      setStep(0);
      setBusy(false);
      return;
    }
    // The rest dresses the adventure up; a part that fails is left for
    // the designer, where it can be set again, rather than lost.
    try {
      await updateAdventure(made, {
        language,
        ...(start ? { start_lat: start.lat, start_lng: start.lng } : {}),
        brief: { ...(audience ? { audience } : {}), ...(pitch.trim() ? { premise: pitch.trim() } : {}) },
      });
      await adventures.saveBrand(made, theme.look as play.Brand);
      await adventures.saveGraph(made, { format: FORMAT_V2, graph: firstGraph(title.trim(), welcome, language, theme) });
    } catch (e) {
      toast.err(e);
    }
    if (example) saveTutorial(made, { baseline: welcome, done: [] });
    location.hash = href({ name: "designer", slug: made });
    onClose();
  };

  const next = () => (step < 3 ? setStep((step + 1) as Step) : void create());
  return (
    <Dialog
      huge
      title={t.t("New adventure")}
      onClose={close}
      onSubmit={next}
      footer={
        <>
          <Button onClick={step === 0 ? close : () => setStep((step - 1) as Step)}>{step === 0 ? t.t("Cancel") : t.t("Back")}</Button>
          <Button type="submit" variant="primary" filled loading={busy} disabled={!basicsDone}>
            {step === 3 ? t.t("Create and design") : step === 2 && !start ? t.t("Skip for now") : t.t("Next")}
          </Button>
        </>
      }
    >
      {example && step === 0 && (
        <p className="newadv__lead">{t.t("This is the tutorial. We have filled in an example to follow: change anything you like, or press Next.")}</p>
      )}
      <ol className="newadv__steps" aria-label={t.t("Steps")}>
        {steps.map((s, i) => (
          <li key={s} className={i === step ? "is-on" : i < step ? "is-done" : ""}>
            <button type="button" disabled={!basicsDone && i > 0} onClick={() => setStep(i as Step)} aria-current={i === step ? "step" : undefined}>
              <span className="newadv__n">{i + 1}</span>
              {s}
            </button>
          </li>
        ))}
      </ol>
      <div className="newadv">
        <div className="newadv__body">
          {step === 0 && (
            <>
              <Field label={t.t("Title")} error={slugError ?? undefined}>
                <input
                  autoFocus
                  required
                  value={title}
                  placeholder={t.t("e.g. “The Lost Vault”")}
                  onChange={(e) => {
                    setTitle(e.target.value);
                    setSlugError(null);
                    if (!slugEdited) setSlug(slugFrom(e.target.value));
                  }}
                />
              </Field>
              <Field label={t.t("One line about it")} hint={t.t("What teams will do, and roughly how long it takes. It is the first thing they read.")}>
                <textarea rows={2} value={pitch} maxLength={300} placeholder={t.t("Help Erik find his stolen elephants before dark!")} onChange={(e) => setPitch(e.target.value)} />
              </Field>
              <div className="newadv__pair">
                <Field label={t.t("Language")}>
                  <select value={language} onChange={(e) => setLanguage(e.target.value)}>
                    {LANGUAGES.map((l) => (
                      <option key={l.code} value={l.code}>{l.name}</option>
                    ))}
                  </select>
                </Field>
                <Field label={t.t("Who is it for?")}>
                  <div className="seg" role="radiogroup">
                    {AUDIENCES.map((a) => (
                      <button key={a.value} type="button" role="radio" aria-checked={audience === a.value} className={audience === a.value ? "is-on" : undefined} onClick={() => setAudience(audience === a.value ? "" : a.value)}>
                        {t.t(a.label)}
                      </button>
                    ))}
                  </div>
                </Field>
              </div>
              <details className="adv-more" open={badSlug || !!slugError}>
                <summary>{t.t("Link name")}</summary>
                <Field label={t.t("Short name")} error={badSlug ? t.t("Lowercase a–z, digits, and hyphens only.") : undefined} hint={t.t("Used in links. Lowercase a–z, digits and hyphens.")}>
                  <input
                    value={slug}
                    placeholder="the-lost-vault"
                    aria-invalid={badSlug || undefined}
                    onChange={(e) => {
                      setSlug(e.target.value);
                      setSlugEdited(e.target.value !== "");
                      setSlugError(null);
                    }}
                  />
                </Field>
              </details>
            </>
          )}
          {step === 1 && (
            <StarterGallery
              selected={theme.id}
              sample={{ title: shownTitle, text: welcome }}
              onPick={(name, look, id) => {
                setTheme({ id, name, look });
                setFeedback(null);
              }}
            />
          )}
          {step === 2 && (
            <>
              <p className="newadv__lead">{t.t("Where do teams meet? Click the map to put the starting point there. You can also set it later, or skip it.")}</p>
              <PointMap value={start} onChange={setStart} />
              {start && (
                <div className="newadv__row">
                  <span className="field__hint">
                    {start.lat.toFixed(5)}, {start.lng.toFixed(5)}
                  </span>
                  <Button onClick={() => setStart(null)}>{t.t("No starting point")}</Button>
                </div>
              )}
            </>
          )}
          {step === 3 && (
            <dl className="newadv__summary">
              <dt>{t.t("Title")}</dt>
              <dd>{title.trim()}</dd>
              {pitch.trim() && (
                <>
                  <dt>{t.t("One line about it")}</dt>
                  <dd>{pitch.trim()}</dd>
                </>
              )}
              <dt>{t.t("Language")}</dt>
              <dd>{LANGUAGES.find((l) => l.code === language)?.name ?? language}</dd>
              {audience && (
                <>
                  <dt>{t.t("Who is it for?")}</dt>
                  <dd>{t.t(AUDIENCES.find((a) => a.value === audience)!.label)}</dd>
                </>
              )}
              <dt>{t.t("The look")}</dt>
              <dd>{theme.id === "default" ? t.t("Default look") : theme.name}</dd>
              <dt>{t.t("Where it starts")}</dt>
              <dd>{start ? `${start.lat.toFixed(5)}, ${start.lng.toFixed(5)}` : t.t("Not set yet")}</dd>
              <dt>{t.t("Link name")}</dt>
              <dd>
                <code>{slug}</code>
              </dd>
            </dl>
          )}
        </div>
        <aside className="newadv__preview" aria-label={t.t("Preview")}>
          <PhoneFrame>
            <PlayView view={view} live={false} preview brand={theme.look as play.Brand} feedback={feedback} onAction={() => {}} />
          </PhoneFrame>
          <div className="newadv__try">
            <Button onClick={() => setFeedback({ tone: "good", text: t.t("Correct!"), burst: { kind: "celebrate", head: t.t("Correct!") } })}>{t.t("Try a right answer")}</Button>
          </div>
        </aside>
      </div>
    </Dialog>
  );
}

/**
 * The new adventure's first graph: a start, a welcome to read with the
 * title and the line about it, and an end — a route to build on, playable
 * at once. A chosen theme comes along as the first saved theme.
 */
function firstGraph(title: string, welcome: string, language: string, theme: { id: string; name: string; look: ThemeLook }): Graph {
  return {
    nodes: [
      { slug: "start", type: "start", x: 0, y: 0 },
      { slug: "welcome", type: "story", title, x: 336, y: 0, story: { blocks: [{ id: "title", kind: "title" }, { id: "b1", kind: "text", text: welcome }] } },
      { slug: "end", type: "end", x: 672, y: 0, end: { outcome: "complete" } },
    ],
    edges: [
      { from: "start", from_port: "out", to: "welcome" },
      { from: "welcome", from_port: "out", to: "end" },
    ],
    language,
    ...(theme.id === "default" ? {} : { themes: [{ slug: "theme-1", name: theme.name || "Theme 1", look: theme.look }] }),
  } as Graph;
}


/** The tutorial's adventure, in the author's language. */
function exampleOf(t: ReturnType<typeof useT>) {
  const jungle = STARTER_THEMES.find((s) => s.id === "jungle");
  return {
    title: t.t("Erik's stolen elephants"),
    pitch: t.t("Help Erik find his stolen elephants before dark! About 45 minutes in the park."),
    theme: jungle ? { id: jungle.id, name: t.t(jungle.label), look: jungle.look as ThemeLook } : { id: "default", name: "", look: DEFAULT_LOOK as ThemeLook },
  };
}

/**
 * Creates under the short name asked for or, when `retry`, the first of
 * name-2, name-3… that is free: the tutorial may be followed more than
 * once, and its author never chose the name.
 */
async function createFree(slug: string, retry: boolean, create: (slug: string) => Promise<{ slug: string }>): Promise<string> {
  for (let n = 1; ; n++) {
    try {
      return (await create(n === 1 ? slug : `${slug}-${n}`)).slug;
    } catch (e) {
      if (!retry || n >= 20 || !(e instanceof ApiError && e.status === 409)) throw e;
    }
  }
}
