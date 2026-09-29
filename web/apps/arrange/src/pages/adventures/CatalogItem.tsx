import { useState } from "react";
import { CalendarPlus, ChevronLeft, Coins, Eye, Plus } from "lucide-react";
import { canAuthor } from "@zonventure/api";
import { flagFor } from "@zonventure/i18n";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { useTopBarTitle } from "../../topbar.tsx";
import { href } from "../../router.ts";
import { Button, Card, ErrorBox, Facts, ICON, Loading, Page, PageHeader, Pill, useLoad } from "../../ui/index.ts";
import { NewEventDialog } from "../events/NewEventDialog.tsx";
import { getAdventure, listAdventures, listAssignments, type Assignment } from "./api.ts";
import { AdventureMap } from "./AdventureMap.tsx";
import { PickPassDialog } from "./dialogs.tsx";
import { computeFacts, factPages } from "./facts.ts";
import { useAdoptAdventure, type Schedule } from "./List.tsx";
import { Cover } from "./parts.tsx";

/**
 * A catalog adventure's read-about page: enough to judge whether it
 * fits an event — where it plays, how long, what media and hints, what
 * players need — without showing a puzzle the org hasn't paid for.
 */
export function CatalogItemPage({ slug }: { slug: string }) {
  const t = useT();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const load = useLoad(
    (signal) => Promise.all([getAdventure(slug, signal), listAssignments(signal).catch(() => [] as Assignment[]), listAdventures(signal).catch(() => [])]),
    [slug],
  );
  useTopBarTitle(load.data?.[0].title ?? null);
  const { adopt, busy } = useAdoptAdventure();
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [picking, setPicking] = useState(false);
  const back = { href: href({ name: "catalog" }), label: t.t("Catalog") };

  if (load.error && !load.data) {
    return (
      <Page>
        <PageHeader title={t.t("Catalog")} back={back} />
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      </Page>
    );
  }
  if (!load.data) return <Page><Loading label={t.t("Loading…")} /></Page>;

  const [a, assignments, rows] = load.data;
  const passes = assignments.filter((x) => x.adventure_slug === a.slug && !x.event_id);
  const shared = !!rows.find((r) => r.slug === a.slug)?.shared;
  const pages = factPages(a);
  const facts = computeFacts(t, pages);
  const accent = a.colors?.accent;

  const openSchedule = () => {
    if (passes.length === 1) setSchedule({ assignmentId: passes[0]!.id });
    else if (passes.length > 1) setPicking(true);
    else setSchedule({ slug: a.slug, ownsIt: !shared });
  };

  const needs = [t.t("A smartphone with a modern browser (Safari, Chrome, Firefox).")];
  if (facts.geo.kind !== "anywhere") needs.push(t.t("GPS / location services enabled — some puzzles only unlock when the team reaches a specific spot."));
  if (facts.audioPages > 0)
    needs.push(t.plural(facts.audioPages, { one: t.t("Audio output — {{n}} puzzle includes audio clips. Headphones recommended in noisy venues."), other: t.t("Audio output — {{n}} puzzles include audio clips. Headphones recommended in noisy venues.") }));
  if (facts.imagePages > 0)
    needs.push(t.plural(facts.imagePages, { one: t.t("Bandwidth for image downloads — {{n}} puzzle includes images."), other: t.t("Bandwidth for image downloads — {{n}} puzzles include images.") }));

  const glance = [
    { label: t.t("Puzzles"), value: String(pages.length) },
    { label: t.t("Estimated playtime"), value: facts.playtime },
    { label: t.t("Location"), value: facts.geo.shortLabel },
    { label: t.t("Media"), value: facts.mediaLabel },
    { label: t.t("Hints"), value: facts.hintsLabel },
    ...(a.language ? [{ label: t.t("Language"), value: `${flagFor(a.language) ? flagFor(a.language) + " " : ""}${a.language.toUpperCase()}` }] : []),
  ];

  return (
    <Page>
      <header className="adv-hero">
        <a className="cs-back" href={back.href}>
          <ChevronLeft size={16} aria-hidden />
          {back.label}
        </a>
        <div className="adv-hero__row">
          <Cover title={a.title} src={a.avatar_url} size={96} accent={accent} />
          <div className="adv-hero__text">
            <h1 className="cs-title">{a.title}</h1>
            <p className="adv-hero__meta">
              {a.author && <span>{a.author}</span>}
              <span className="adv-mono">{a.slug}</span>
            </p>
            {(shared || passes.length > 0) && (
              <div className="adv-hero__pills">
                {shared && <Pill tone="accent" title={t.t("Commissioned for your organization")}>{t.t("Shared")}</Pill>}
                {passes.length > 0 && <Pill tone="accent">{t.plural(passes.length, { one: t.t("{{n}} event pass ready"), other: t.t("{{n}} event passes ready") })}</Pill>}
              </div>
            )}
          </div>
        </div>
        <div className="adv-hero__actions">
          {!a.graph && pages.length > 0 && (
            <a className="btn adv-btn" href={`/play?preview=${encodeURIComponent(a.slug)}&page=0`} target="_blank" rel="noopener">
              <Eye size={ICON.md} aria-hidden />
              {t.t("Preview")}
            </a>
          )}
          {shared || passes.length > 0 ? (
            <Button variant="primary" icon={<CalendarPlus size={ICON.md} aria-hidden />} onClick={openSchedule}>{t.t("Schedule event")}</Button>
          ) : (
            <Button icon={<Coins size={ICON.md} aria-hidden />} onClick={openSchedule} title={t.t("Run it as it is: buy an event pass and schedule")}>{t.t("Buy an event pass")}</Button>
          )}
          {user && canAuthor(user) && (
            <Button
              variant={shared || passes.length > 0 ? "secondary" : "primary"}
              icon={<Plus size={ICON.md} aria-hidden />}
              loading={busy === a.slug}
              onClick={() => adopt(a.slug)}
              title={t.t("Copy it into your Adventures to adjust it")}
            >
              {t.t("Use this adventure")}
            </Button>
          )}
        </div>
        {a.description && <p className="adv-pre">{a.description}</p>}
      </header>

      <Card title={t.t("At a glance")}>
        <Facts items={glance} />
      </Card>

      <Card title={t.t("Where to play")}>
        <p>{facts.geo.blurb}</p>
        {facts.geo.points.length > 0 && (
          <AdventureMap faint pins={facts.geo.points.map((p) => ({ ...p, title: "", gated: true }))} accent={accent} label={t.t("Where to play")} height={240} />
        )}
      </Card>

      <Card title={t.t("What players need")}>
        <ul className="adv-list">
          {needs.map((n) => <li key={n}>{n}</li>)}
        </ul>
      </Card>

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
