import { useRef, useState, type ReactNode } from "react";
import { CalendarPlus, ChevronLeft, Coins, Copy, Download, ExternalLink, Eye, MapPin, Pencil, Trash2, Workflow } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { useTopBarTitle } from "../../topbar.tsx";
import { CATALOG_ENABLED, consoleHref, href } from "../../router.ts";
import { Button, Card, ErrorBox, ICON, Loading, Page, PageHeader, Pill, Row, Stat, Stats, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { NewEventDialog } from "../events/NewEventDialog.tsx";
import { deleteAdventure, duplicateAdventure, exportURL, getAdventure, getGraphState, isPublisher, listAdventures, listAssignments, type Accept, type AdventureDetail, type Assignment, type DetailPage } from "./api.ts";
import { AdventureMap } from "./AdventureMap.tsx";
import { EditDetailsDialog, PickPassDialog } from "./dialogs.tsx";
import { extentKm, formatKm, hintDelay, pinsOf } from "./facts.ts";
import type { Schedule } from "./List.tsx";
import { Cover, LanguagePill, puzzleCount } from "./parts.tsx";
import { MoreMenu, type MenuItem } from "../event/Menu.tsx";

/**
 * An adventure's own page: what it is, where it happens and what it is
 * made of, and — for its authors — the way into the designer and the
 * adventure's housekeeping (details, cover, copy, export, delete).
 */
export function AdventurePage({ slug }: { slug: string }) {
  const t = useT();
  // The list row says whether the org holds it through a commission; the detail payload doesn't.
  const load = useLoad(
    (signal) => Promise.all([getAdventure(slug, signal), listAssignments(signal).catch(() => [] as Assignment[]), listAdventures(signal).catch(() => [])]),
    [slug],
  );
  useTopBarTitle(load.data?.[0].title ?? null);

  if (load.error && !load.data) {
    return (
      <Page>
        <PageHeader title={t.t("Adventure")} back={{ href: href({ name: "adventures" }), label: t.t("Adventures") }} />
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      </Page>
    );
  }
  if (!load.data) return <Page><Loading label={t.t("Loading…")} /></Page>;
  const [a, assignments, rows] = load.data;
  return <Detail a={a} shared={!a.mine_to_edit && !!rows.find((r) => r.slug === a.slug)?.shared} passes={assignments.filter((x) => x.adventure_slug === a.slug && !x.event_id)} reload={load.reload} />;
}

function Detail({ a, shared, passes, reload }: { a: AdventureDetail; shared: boolean; passes: Assignment[]; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [picking, setPicking] = useState(false);
  const [editing, setEditing] = useState(false);
  const [cover, setCover] = useState(a.avatar_url);
  const [copying, setCopying] = useState(false);
  const [focus, setFocus] = useState<{ lat: number; lng: number; n: number } | null>(null);
  const mapCard = useRef<HTMLDivElement>(null);

  const g = a.graph ?? null;
  const pages = g ? [] : (a.pages ?? []);
  const enabled = pages.filter((p) => !p.disabled);
  const pins = pinsOf(a, t);
  const zoneNames = new Map((g?.zones ?? []).map((z) => [z.slug, z.name || z.slug]));
  // Made in the designer but not published: no pages and no graph to describe.
  const designed = !g && pages.length === 0;
  const accent = a.colors?.accent;
  // The org's own adventure, whoever may edit it: scheduled through the buy-then-schedule flow.
  const own = a.mine_to_edit || (a.org_id != null && a.org_id === user?.org_id);

  const puzzles = g ? g.puzzle_count : enabled.length;
  const places = g ? new Set(g.steps.map((s) => s.zone).filter((z) => z && zoneNames.has(z))).size : enabled.filter((p) => p.gated).length;
  const km = extentKm(pins);

  const start = a.start_lat != null && a.start_lng != null
    ? { lat: a.start_lat, lng: a.start_lng, label: a.start_label || "", inferred: false }
    : (() => {
        const p = g ? null : enabled.find((x) => x.gated && (x.location.lat || x.location.lng));
        return p ? { lat: p.location.lat, lng: p.location.lng, label: p.title || "", inferred: true } : null;
      })();
  const firstPlace = g ? (g.zones[0] ?? null) : (enabled.find((x) => x.gated && (x.location.lat || x.location.lng))?.location ?? null);

  const openSchedule = () => {
    if (passes.length === 1) setSchedule({ assignmentId: passes[0]!.id });
    else if (passes.length > 1) setPicking(true);
    else if (own) setSchedule({ slug: a.slug, ownsIt: true });
    else setSchedule({ slug: a.slug, ownsIt: !shared });
  };

  const duplicate = async () => {
    if (copying) return;
    setCopying(true);
    try {
      const r = await duplicateAdventure(a.slug);
      toast.ok(t.t("Duplicated. This is the copy — rename it under Edit details."));
      location.hash = href({ name: "adventure", slug: r.slug });
    } catch (e) {
      toast.err(e);
    } finally {
      setCopying(false);
    }
  };

  const remove = async () => {
    const pw = await dialogs.password({
      title: t.t("Delete adventure “{{title}}”?", { title: a.title }),
      message: t.t("This removes the adventure and all its puzzles. Enter your password to confirm."),
      confirmLabel: t.t("Delete adventure"),
      danger: true,
    });
    if (!pw) return;
    try {
      await deleteAdventure(a.slug, pw);
      toast.ok(t.t("Deleted"));
      location.hash = href({ name: "adventures" });
    } catch (e) {
      toast.err(e);
    }
  };

  const mapped = !designed && pins.length > 0;
  const openInMaps = start && (
    <a className="btn adv-btn sm" href={`https://www.openstreetmap.org/?mlat=${start.lat}&mlon=${start.lng}#map=17/${start.lat}/${start.lng}`} target="_blank" rel="noopener" title={t.t("Open the starting point in maps")}>
      <ExternalLink size={ICON.sm} aria-hidden />
      {t.t("Open in maps")}
    </a>
  );
  const startLine = start && (
    <div className="adv-start">
      <MapPin size={ICON.sm} aria-hidden />
      <span>
        <strong>{mapped ? t.t("Start: {{place}}", { place: start.label || t.t("Start here") }) : start.label || t.t("Start here")}</strong>{" "}
        <span className="adv-mono adv-muted">{start.lat.toFixed(5)}, {start.lng.toFixed(5)}</span>
        {start.inferred && <span className="cs-field__hint adv-block">{t.t("Inferred from the first gated puzzle. Author: set an explicit starting location in the adventure editor.")}</span>}
      </span>
    </div>
  );

  const showOnMap = (p: { lat: number; lng: number }) => {
    setFocus({ ...p, n: Date.now() });
    mapCard.current?.scrollIntoView({ behavior: "smooth", block: "center" });
  };

  const pills: ReactNode[] = [];
  if (CATALOG_ENABLED && a.mine_to_edit && isPublisher(user)) {
    pills.push(
      <Pill key="listed" tone={a.published ? "good" : "neutral"} title={a.published ? t.t("Every organization finds it in the Catalog.") : t.t("Other organizations can't find it until the operator lists it.")}>
        {a.published ? t.t("In the Catalog") : t.t("Not in the Catalog")}
      </Pill>,
    );
  }
  if (shared) pills.push(<Pill key="shared" tone="accent" title={t.t("Commissioned for your organization")}>{t.t("Shared")}</Pill>);
  if (passes.length) pills.push(<Pill key="passes" tone="accent">{t.plural(passes.length, { one: t.t("{{n}} event pass ready"), other: t.t("{{n}} event passes ready") })}</Pill>);
  if (a.language) pills.push(<LanguagePill key="lang" code={a.language} />);
  if ((a.running_event_count ?? 0) > 0)
    pills.push(<Pill key="run" tone="warn">{t.plural(a.running_event_count!, { one: t.t("1 event running now"), other: t.t("{{n}} events running now") })}</Pill>);

  const more: MenuItem[] = [];
  if (a.mine_to_edit) {
    if (enabled.length > 0) more.push({ label: t.t("Preview as player"), icon: <Eye size={ICON.sm} aria-hidden />, href: `/play?preview=${encodeURIComponent(a.slug)}`, target: "_blank" });
    more.push(
      { label: t.t("Edit details"), icon: <Pencil size={ICON.sm} aria-hidden />, title: t.t("Title, short name, description, intro, start and cover"), onClick: () => setEditing(true) },
      { label: t.t("Duplicate"), icon: <Copy size={ICON.sm} aria-hidden />, title: t.t("Make an independent copy in your organization"), onClick: () => void duplicate() },
      { label: t.t("Export"), icon: <Download size={ICON.sm} aria-hidden />, href: exportURL(a.slug), download: `${a.slug}.zvadv.zip`, title: t.t("Download this adventure as a portable .zip") },
      { separator: true },
      { label: t.t("Delete adventure"), icon: <Trash2 size={ICON.sm} aria-hidden />, danger: true, title: t.t("Delete the adventure and all its puzzles. An adventure that has had events can’t be deleted."), onClick: () => void remove() },
    );
  }

  const actions = (
    <>
      {a.mine_to_edit && (
        <a className="btn adv-btn primary" href={href({ name: "designer", slug: a.slug })} title={t.t("Design the adventure as a flow of steps")}>
          <Workflow size={ICON.md} aria-hidden />
          {t.t("Open designer")}
        </a>
      )}
      {own || shared || passes.length ? (
        !designed && (
          <Button variant={a.mine_to_edit ? "secondary" : "primary"} icon={<CalendarPlus size={ICON.md} aria-hidden />} onClick={openSchedule}>
            {t.t("Schedule event")}
          </Button>
        )
      ) : (
        <Button variant="primary" icon={<Coins size={ICON.md} aria-hidden />} onClick={openSchedule}>
          {t.t("Buy an event pass")}
        </Button>
      )}
      {a.mine_to_edit && !g && pages.length > 0 && (
        <a className="btn adv-btn" href={consoleHref(`library/${encodeURIComponent(a.slug)}`)} title={t.t("The older editor: puzzles in lanes")}>
          <Pencil size={ICON.md} aria-hidden />
          {t.t("Classic editor")}
        </a>
      )}
      {!a.mine_to_edit && enabled.length > 0 && (
        <a className="btn adv-btn" href={`/play?preview=${encodeURIComponent(a.slug)}`} target="_blank" rel="noopener">
          <Eye size={ICON.md} aria-hidden />
          {t.t("Preview as player")}
        </a>
      )}
      {more.length > 0 && <MoreMenu label={t.t("More")} items={more} />}
    </>
  );

  return (
    <Page>
      <header className="adv-hero">
        <a className="cs-back" href={href({ name: "adventures" })}>
          <ChevronLeft size={16} aria-hidden />
          {t.t("Adventures")}
        </a>
        <div className="adv-hero__row">
          {a.mine_to_edit ? (
            <button type="button" className="adv-hero__cover" onClick={() => setEditing(true)} title={t.t("Change the cover picture")} aria-label={t.t("Change the cover picture")}>
              <Cover title={a.title} src={cover} size={96} accent={accent} />
            </button>
          ) : (
            <Cover title={a.title} src={cover} size={96} accent={accent} />
          )}
          <div className="adv-hero__text">
            <h1 className="cs-title">{a.title}</h1>
            <p className="adv-hero__meta">
              {a.author && <span>{a.author}</span>}
              <span className="adv-mono">{a.slug}</span>
            </p>
            {pills.length > 0 && <div className="adv-hero__pills">{pills}</div>}
          </div>
        </div>
        <div className="adv-hero__actions">{actions}</div>
      </header>

      {!designed && (
        <Stats>
          <Stat value={String(puzzles)} label={t.plural(puzzles, { one: t.t("puzzle"), other: t.t("puzzles") })} />
          <Stat value={String(places)} label={t.plural(places, { one: t.t("place to reach"), other: t.t("places to reach") })} />
          {km > 0 && <Stat value={formatKm(km, t.current())} label={t.t("across")} />}
        </Stats>
      )}

      {(a.description || a.intro_text?.trim() || a.outro_text?.trim()) && (
        <Card title={t.t("About")}>
          {a.description && <p className="adv-pre">{a.description}</p>}
          {a.intro_text?.trim() && (
            <div>
              <h3 className="adv-h3">{t.t("Intro shown to players")}</h3>
              <Prose html={a.intro_html} text={a.intro_text} />
            </div>
          )}
          {a.outro_text?.trim() && (
            <div>
              <h3 className="adv-h3">{t.t("Outro on completion")}</h3>
              <Prose html={a.outro_html} text={a.outro_text} />
            </div>
          )}
        </Card>
      )}

      {designed && <DesignedSteps slug={a.slug} canRead={a.mine_to_edit} />}

      {mapped ? (
        <div ref={mapCard}>
          <Card title={t.t("Map")} subtitle={g ? t.t("Where the steps happen: the zones teams must reach.") : t.t("All puzzle locations. Show one from its card below.")} actions={start && openInMaps}>
            {start && startLine}
            <AdventureMap pins={pins} accent={accent} focus={focus} label={t.t("Map")} />
          </Card>
        </div>
      ) : start && (
        <Card title={t.t("Start here")} actions={openInMaps}>
          {startLine}
          <AdventureMap pins={[{ title: start.label || t.t("Start here"), lat: start.lat, lng: start.lng, gated: false }]} accent={accent} label={t.t("Start here")} height={200} />
        </Card>
      )}

      {g && (
        <Card title={t.t("Steps")} flush>
          {g.steps.length === 0 ? (
            <p className="adv-pad adv-muted">{t.t("Nothing designed yet.")}</p>
          ) : (
            <ol className="adv-steps">
              {g.steps.map((s, i) => (
                <li key={s.slug + i} className="adv-step">
                  <Pill tone={s.type === "puzzle" ? "accent" : "neutral"}>{stepKind(t, s.type)}</Pill>
                  <strong>{s.title || s.slug}</strong>
                  {s.zone && zoneNames.has(s.zone) && (
                    <span className="adv-muted adv-step__zone">
                      <MapPin size={ICON.xs} aria-hidden />
                      {zoneNames.get(s.zone)}
                    </span>
                  )}
                </li>
              ))}
            </ol>
          )}
          {g.has_quiz && <p className="adv-pad adv-muted">{t.t("Includes a quiz walk.")}</p>}
        </Card>
      )}

      {!g && !designed && <Puzzles a={a} pages={enabled} onMap={showOnMap} />}

      {editing && (
        <EditDetailsDialog
          slug={a.slug}
          accent={accent}
          firstPlace={firstPlace ? { lat: firstPlace.lat, lng: firstPlace.lng } : null}
          onClose={() => setEditing(false)}
          onCover={(url) => setCover(url || undefined)}
          onSaved={(next) => {
            setEditing(false);
            if (next !== a.slug) location.hash = href({ name: "adventure", slug: next });
            else reload();
          }}
        />
      )}
      {picking && (
        <PickPassDialog
          title={a.title}
          passes={passes}
          onClose={() => setPicking(false)}
          onPick={(id) => {
            setPicking(false);
            setSchedule({ assignmentId: id });
          }}
        />
      )}
      {schedule && <NewEventDialog {...schedule} onClose={() => setSchedule(null)} />}
    </Page>
  );
}

function stepKind(t: ReturnType<typeof useT>, type: string): string {
  switch (type) {
    case "puzzle": return t.t("Puzzle");
    case "story": return t.t("Story");
    case "hub": return t.t("Choice");
    case "end": return t.t("End");
    default: return type;
  }
}

/** Server-rendered markdown (the same goldmark pipeline as puzzle prompts), else the raw text. */
function Prose({ html, text }: { html?: string | undefined; text?: string | undefined }) {
  return html ? <div className="adv-prose" dangerouslySetInnerHTML={{ __html: html }} /> : <p className="adv-pre">{text}</p>;
}

/** An adventure made in the designer, not yet published: how much is there, and whether events can play it. */
function DesignedSteps({ slug, canRead }: { slug: string; canRead: boolean }) {
  const t = useT();
  const load = useLoad(
    (signal) =>
      canRead
        ? getGraphState(slug, signal).catch((e: unknown) => {
            if (e instanceof ApiError && e.code === "graph_not_found") return null;
            throw e;
          })
        : Promise.resolve(null),
    [slug, canRead],
  );
  let body: ReactNode;
  if (load.loading && !load.data) body = <Loading label={t.t("Loading…")} />;
  else if (load.error) body = <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  else {
    const nodes = load.data?.envelope?.graph?.nodes ?? [];
    const puzzles = nodes.filter((n) => n.type === "puzzle").length;
    const steps = nodes.filter((n) => n.type !== "start" && n.type !== "end").length;
    const pub = load.data?.published;
    body = (
      <>
        <p>{steps ? t.plural(steps, { one: t.t("{{n}} step"), other: t.t("{{n}} steps") }) + " · " + puzzleCount(t, puzzles) : t.t("Nothing designed yet.")}</p>
        <p className="adv-muted">{pub ? t.t("Published: events that start now play version {{n}}.", { n: pub.version }) : t.t("Not published yet, so events can't play it. Publish it from the designer.")}</p>
      </>
    );
  }
  return <Card title={t.t("Steps")}>{body}</Card>;
}

/** A lane adventure's puzzles, grouped by lane when it has more than one. */
function Puzzles({ a, pages, onMap }: { a: AdventureDetail; pages: DetailPage[]; onMap: (p: { lat: number; lng: number }) => void }) {
  const t = useT();
  const lanes = [...(a.lanes ?? [])].sort((x, y) => x.idx - y.idx);
  if (pages.length === 0) {
    return (
      <Card title={t.t("Puzzles")}>
        <p className="adv-muted">{t.t("No puzzles yet.")}</p>
      </Card>
    );
  }
  if (lanes.length <= 1) {
    return (
      <Card title={t.t("Puzzles")}>
        <ol className="adv-puzzles">{pages.map((p, i) => <PuzzleCard key={p.index} p={p} n={i + 1} onMap={onMap} />)}</ol>
      </Card>
    );
  }
  const byLane = new Map<number, DetailPage[]>(lanes.map((l) => [l.id, []]));
  const orphans: DetailPage[] = [];
  for (const p of pages) (byLane.get(p.lane_id ?? -1) ?? orphans).push(p);
  let n = 0;
  return (
    <Card title={t.t("Puzzles")}>
      {lanes.map((l) => {
        const lp = byLane.get(l.id) ?? [];
        if (lp.length === 0) return null;
        return (
          <section key={l.id} className="adv-lane">
            <h3 className="adv-h3">
              {l.name || t.t("Lane {{n}}", { n: l.idx + 1 })}
              <Pill tone={l.mode === "shuffled" ? "info" : "neutral"}>{l.mode === "shuffled" ? t.t("Shuffled") : t.t("Linear")}</Pill>
              <span className="adv-muted adv-small">{puzzleCount(t, lp.length)}</span>
            </h3>
            <ol className="adv-puzzles" start={n + 1}>{lp.map((p) => <PuzzleCard key={p.index} p={p} n={++n} onMap={onMap} />)}</ol>
          </section>
        );
      })}
      {orphans.length > 0 && (
        <section className="adv-lane">
          <h3 className="adv-h3">{t.t("Unassigned")}</h3>
          <ol className="adv-puzzles" start={n + 1}>{orphans.map((p) => <PuzzleCard key={p.index} p={p} n={++n} onMap={onMap} />)}</ol>
        </section>
      )}
    </Card>
  );
}

function PuzzleCard({ p, n, onMap }: { p: DetailPage; n: number; onMap: (p: { lat: number; lng: number }) => void }) {
  const t = useT();
  const [answers, setAnswers] = useState(false);
  const { lat, lng, radius_m } = p.location;
  return (
    <li className="adv-pz">
      <div className="adv-pz__head">
        <span className="adv-pz__num" aria-hidden>{n}</span>
        <strong className="adv-pz__title">{p.title || t.t("(untitled)")}</strong>
        {p.gated && <Pill tone="warn">{t.t("gated")}</Pill>}
      </div>
      {!!p.images?.length && (
        <div className="adv-pz__images">
          {p.images.map((im) => <img key={im.id} src={im.url} alt={im.alt_text || ""} title={im.alt_text || undefined} loading="lazy" />)}
        </div>
      )}
      {p.prompt_html ? <div className="adv-prose" dangerouslySetInnerHTML={{ __html: p.prompt_html }} /> : p.prompt ? <p className="adv-pre">{p.prompt}</p> : null}
      {!!p.hints?.length && (
        <details className="adv-pz__hints">
          <summary>{t.plural(p.hints.length, { one: t.t("{{n}} hint"), other: t.t("{{n}} hints") })}</summary>
          {p.hints.map((h, i) => (
            <div key={i} className="adv-pz__hint">
              <div className="adv-muted adv-small">{t.t("Hint {{i}}", { i: i + 1 })} · {hintDelay(t, h.delay_seconds || 0)}</div>
              {h.text_html && <div className="adv-prose" dangerouslySetInnerHTML={{ __html: h.text_html }} />}
              {!!h.images?.length && <div className="adv-pz__images">{h.images.map((im) => <img key={im.id} src={im.url} alt={im.alt_text || ""} loading="lazy" />)}</div>}
            </div>
          ))}
        </details>
      )}
      <Row gap="sm">
        {(lat || lng) ? (
          <Button size="sm" variant="ghost" icon={<MapPin size={ICON.sm} aria-hidden />} onClick={() => onMap({ lat, lng })}>
            {t.t("Show on map")}{radius_m ? t.t(" · gate {{r}} m", { r: radius_m }) : ""}
          </Button>
        ) : null}
        {!!p.accept?.length && (
          <Button size="sm" variant="ghost" icon={<Eye size={ICON.sm} aria-hidden />} aria-expanded={answers} onClick={() => setAnswers((v) => !v)}>
            {answers ? t.t("Hide answers") : t.t("Show answers")}
          </Button>
        )}
      </Row>
      {answers && p.accept && <p className="adv-answers">{p.accept.map((x, i) => <AcceptChip key={i} a={x} />)}</p>}
    </li>
  );
}

function AcceptChip({ a }: { a: Accept | string }) {
  const t = useT();
  if (typeof a === "string") return <span className="adv-chip">{a}</span>;
  if (a.regex) return <span className="adv-chip adv-mono" title={t.t("Regex pattern (anchored, case-insensitive)")}>re: {a.text}</span>;
  return <span className="adv-chip">{a.text}</span>;
}
