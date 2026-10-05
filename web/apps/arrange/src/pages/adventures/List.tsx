import { useEffect, useState, type ReactNode } from "react";
import { CalendarPlus, Coins, Compass, MapPin, Plus, Upload, Workflow } from "lucide-react";
import { canAuthor } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { CATALOG_ENABLED, href, navigate } from "../../router.ts";
import { Button, Empty, ErrorBox, HelpPopover, ICON, Loading, Page, PageHeader, Pill, useLoad, useToast } from "../../ui/index.ts";
import { NewEventDialog } from "../events/NewEventDialog.tsx";
import { NewQuestDialog } from "../events/NewQuestDialog.tsx";
import { duplicateAdventure, isPublisher, listAdventures, listAssignments, type AdventureRow, type Assignment } from "./api.ts";
import { CreateAdventureDialog, PickPassDialog, useImportAdventure } from "./dialogs.tsx";
import { AdventureTabs, Cover, LanguagePill, puzzleCount } from "./parts.tsx";

/** What the schedule dialog opens with. */
export type Schedule = { slug?: string; assignmentId?: number; ownsIt?: boolean };

/** One adventure in a grid: the title is the link, the actions sit on top of it. */
function AdventureCard({ a, to, tags, actions }: { a: AdventureRow; to: string; tags: ReactNode; actions?: ReactNode }) {
  return (
    <article className="adv-card">
      <div className="adv-card__head">
        <Cover title={a.title} src={a.avatar_url} size={56} />
        <h2 className="adv-card__title">
          <a href={to} className="adv-card__link">{a.title || a.slug}</a>
        </h2>
        <div className="adv-card__tags">{tags}</div>
      </div>
      {a.description && <p className="adv-card__desc">{a.description}</p>}
      <p className="adv-card__meta">{a.slug}{a.author ? " · " + a.author : ""}</p>
      {actions && <div className="adv-card__actions">{actions}</div>}
    </article>
  );
}

/**
 * Yours: what the org can run now — what it authors, holds an event
 * pass for, or has been commissioned. Everything else is the Catalog.
 */
export function AdventuresPage() {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const author = !!user && canAuthor(user);
  const load = useLoad((signal) => Promise.all([listAdventures(signal), listAssignments(signal)]), []);
  const [creating, setCreating] = useState(false);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [selling, setSelling] = useState<string | null>(null);
  const [picking, setPicking] = useState<{ a: AdventureRow; passes: Assignment[] } | null>(null);
  const importer = useImportAdventure();

  // Back from checkout: the new pass is in sessionStorage; schedule with it once.
  useEffect(() => {
    let id = 0;
    try {
      id = Number.parseInt(sessionStorage.getItem("zv-just-purchased") ?? "", 10);
      sessionStorage.removeItem("zv-just-purchased");
    } catch {
      /* storage blocked */
    }
    if (id > 0) {
      toast.ok(t.t("Payment received — let's schedule your event."));
      setSchedule({ assignmentId: id });
    }
    // Once, on arrival.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const header = (
    <PageHeader
      title={t.t("Adventures")}
      actions={
        author && (
          <>
            <a className="btn" href="#/field" title={t.t("Share your location with your assistant while you build on site")}>
              <MapPin size={ICON.md} aria-hidden />
              {t.t("Field mode")}
            </a>
            <Button icon={<Upload size={ICON.md} aria-hidden />} onClick={importer.pick} title={t.t("Import an adventure exported as a .zip")}>{t.t("Import")}</Button>
            <Button variant="primary" filled icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New adventure")}</Button>
          </>
        )
      }
    />
  );

  let body: ReactNode;
  if (load.error && !load.data) body = <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  else if (!load.data) body = <Loading label={t.t("Loading…")} />;
  else {
    const [advs, assignments] = load.data;
    const byAdv = new Map<string, { unused: Assignment[]; used: Assignment[] }>();
    for (const x of assignments) {
      if (!x.adventure_slug) continue;
      const b = byAdv.get(x.adventure_slug) ?? { unused: [], used: [] };
      (x.event_id ? b.used : b.unused).push(x);
      byAdv.set(x.adventure_slug, b);
    }
    const bucket = (a: AdventureRow) => byAdv.get(a.slug) ?? { unused: [], used: [] };
    // Yours first, then with a pass ready, then commissioned, then passes spent.
    const score = (a: AdventureRow) => {
      const b = bucket(a);
      return (a.mine_to_edit ? 8 : 0) + (b.unused.length ? 4 : 0) + (a.shared ? 2 : 0) + (b.used.length ? 1 : 0);
    };
    const runnable = advs.filter((a) => a.mine_to_edit || a.shared || byAdv.has(a.slug)).sort((x, y) => score(y) - score(x));

    const scheduleBtn = (onClick: () => void) => (
      <Button size="sm" icon={<CalendarPlus size={ICON.sm} aria-hidden />} onClick={onClick}>{t.t("Schedule event")}</Button>
    );
    // Self-guided needs no pass, only an adventure the org owns or was commissioned.
    const selfGuidedBtn = (a: AdventureRow) => (
      <Button size="sm" icon={<Compass size={ICON.sm} aria-hidden />} title={t.t("New self-guided adventure")} onClick={() => setSelling(a.slug)}>{t.t("Self-guided adventure")}</Button>
    );
    // The pass it holds, else free when commissioned, else buy-then-schedule for its own.
    const actionsFor = (a: AdventureRow) => {
      const b = bucket(a);
      const sell = (a.shared || (a.mine_to_edit && a.playable)) && selfGuidedBtn(a);
      if (b.unused.length === 1) return <>{scheduleBtn(() => setSchedule({ assignmentId: b.unused[0]!.id }))}{sell}</>;
      if (b.unused.length > 1) return <>{scheduleBtn(() => setPicking({ a, passes: b.unused }))}{sell}</>;
      if (a.shared) return <>{scheduleBtn(() => setSchedule({ slug: a.slug }))}{sell}</>;
      if (a.mine_to_edit && !a.playable)
        return (
          <>
            <a className="btn adv-btn sm" href={href({ name: "designer", slug: a.slug })}>
              <Workflow size={ICON.sm} aria-hidden />
              {t.t("Open designer")}
            </a>
            <span className="adv-card__hint">{t.t("Publish it from the designer to schedule events.")}</span>
          </>
        );
      if (a.mine_to_edit) return <>{scheduleBtn(() => setSchedule({ slug: a.slug, ownsIt: true }))}{sell}</>;
      return null;
    };
    const tagsFor = (a: AdventureRow) => {
      const b = bucket(a);
      return (
        <>
          <Pill>{puzzleCount(t, a.page_count)}</Pill>
          {!a.mine_to_edit && a.shared && <Pill tone="accent" title={t.t("Commissioned for your organization")}>{t.t("Shared")}</Pill>}
          {b.unused.length > 0 ? (
            <Pill tone="accent">{t.plural(b.unused.length, { one: t.t("{{n}} event pass ready"), other: t.t("{{n}} event passes ready") })}</Pill>
          ) : b.used.length > 0 ? (
            <Pill>{t.plural(b.used.length, { one: t.t("{{n}} event held"), other: t.t("{{n}} events held") })}</Pill>
          ) : null}
        </>
      );
    };

    body =
      runnable.length === 0 ? (
        <Empty
          title={t.t("No adventures you can use yet.")}
          action={
            CATALOG_ENABLED && (
              <a className="btn adv-btn" href={href({ name: "catalog" })}>
                <Coins size={ICON.md} aria-hidden />
                {t.t("Browse the Catalog")}
              </a>
            )
          }
        >
          {CATALOG_ENABLED
            ? author ? t.t("Create your own or browse the Catalog for published adventures.") : t.t("Browse the Catalog for published adventures.")
            : author ? t.t("Create your own adventure to get started.") : t.t("An organisation admin can create one for you.")}
        </Empty>
      ) : (
        <div className="adv-grid">
          {runnable.map((a) => (
            <AdventureCard key={a.slug} a={a} to={href({ name: "adventure", slug: a.slug })} tags={tagsFor(a)} actions={actionsFor(a)} />
          ))}
        </div>
      );
  }

  return (
    <Page>
      {header}
      <AdventureTabs current="yours" />
      {body}
      {creating && <CreateAdventureDialog onClose={() => setCreating(false)} />}
      {importer.dialog}
      {picking && (
        <PickPassDialog
          title={picking.a.title}
          passes={picking.passes}
          onClose={() => setPicking(null)}
          onPick={(id) => {
            setPicking(null);
            setSchedule({ assignmentId: id });
          }}
        />
      )}
      {schedule && <NewEventDialog {...schedule} onClose={() => setSchedule(null)} />}
      {selling && <NewQuestDialog slug={selling} onClose={() => setSelling(null)} onCreated={(id) => navigate({ name: "event", id })} />}
    </Page>
  );
}

/**
 * Take a catalog adventure into the org: a copy of it, the org's own to
 * adjust and schedule. The server allows it for org admins only.
 */
export function useAdoptAdventure() {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const adopt = async (slug: string) => {
    setBusy(slug);
    try {
      const r = await duplicateAdventure(slug);
      toast.ok(t.t("Added to your Adventures — customise as much or as little as you want, then schedule."));
      location.hash = href({ name: "adventure", slug: r.slug });
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(null);
    }
  };
  return { adopt, busy };
}

/** Published adventures from other organizations. */
export function CatalogPage() {
  const t = useT();
  const { state } = useSession();
  const user = state.status === "authenticated" ? state.user : null;
  const publisher = isPublisher(user);
  const author = !!user && canAuthor(user);
  const load = useLoad((signal) => listAdventures(signal), []);
  const { adopt, busy } = useAdoptAdventure();

  let body: ReactNode;
  if (load.error && !load.data) body = <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  else if (!load.data) body = <Loading label={t.t("Loading…")} />;
  else {
    const catalog = load.data.filter((a) => !a.mine_to_edit && a.published);
    body =
      catalog.length === 0 ? (
        <Empty title={t.t("No published adventures yet.")}>
          {publisher ? t.t("Your organization publishes the catalog, so your adventures are listed under Yours.") : t.t("Check back soon — operators publish new adventures regularly.")}
        </Empty>
      ) : (
        <div className="adv-grid">
          {catalog.map((a) => (
            <AdventureCard
              key={a.slug}
              a={a}
              to={href({ name: "catalog-item", slug: a.slug })}
              tags={
                <>
                  <LanguagePill code={a.language} />
                  <Pill>{puzzleCount(t, a.page_count)}</Pill>
                  {a.shared && <Pill tone="accent" title={t.t("Commissioned for your organization")}>{t.t("Shared")}</Pill>}
                </>
              }
              actions={
                author && (
                  <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} loading={busy === a.slug} disabled={!!busy} onClick={() => adopt(a.slug)}>
                    {t.t("Use this adventure")}
                  </Button>
                )
              }
            />
          ))}
        </div>
      );
  }

  return (
    <Page>
      <PageHeader
        title={t.t("Adventures")}
        subtitle={
          <>
            {t.t("Published adventures from other organizations.")}{" "}
            <HelpPopover
              label={t.t("About the Catalog")}
              body={publisher ? t.t("Your own published adventures are what other organizations find here.") : t.t("Buy an event pass to add one to your own Adventures list.")}
            />
          </>
        }
      />
      <AdventureTabs current="catalog" />
      {body}
    </Page>
  );
}
