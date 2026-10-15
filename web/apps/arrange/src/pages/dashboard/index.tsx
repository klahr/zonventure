import { useState } from "react";
import { ArrowRight, Building2, CalendarClock, Plus, Radio, Ticket, Users } from "lucide-react";
import { canAuthor, get, isGlobalAdmin, readsAsAdmin, runsEvents, siteName, type Whoami } from "@zonventure/api";
import { WIDE, wideSrcSet } from "@zonventure/ui";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { CATALOG_ENABLED, href, navigate } from "../../router.ts";
import { Avatar, Button, Callout, Card, Dialog, ErrorBox, Field, Loading, Page, PageHeader, Pill, Row, Table, errorText, fmtMoney, relTime, useLoad, useToast, type Column } from "../../ui/index.ts";
import { activeMembership, listAdventures, listAssignments, listEvents } from "../events/api.ts";
import { EventsTable } from "../events/parts.tsx";
import { NewEventDialog, canStartEvent, type NewEventProps } from "../events/NewEventDialog.tsx";
import { NewQuestDialog } from "../events/NewQuestDialog.tsx";
import { getConnectStatus } from "../event/api.ts";
import { createOrganization, createOwnOrganization, getCatalog, listOrganizations, listToInvoice, type OrgRow, type PurchaseRow } from "./api.ts";
import { SetupCard, type SetupStepProps } from "./Setup.tsx";
import { Agenda, LiveCard, Tile } from "./Overview.tsx";
import { Onboarding } from "./Onboarding.tsx";
import "./dashboard.css";

export { OnboardingDialog } from "./Onboarding.tsx";

/**
 * The dashboard, by who is looking: the platform admin's deployment
 * overview, the lobby for a user in no organisation yet, or an
 * organisation's events at a glance.
 */
export function DashboardPage() {
  const { state } = useSession();
  if (state.status !== "authenticated") return null;
  const user = state.user;
  return (
    <>
      {readsAsAdmin(user) ? <AdminDashboard full={isGlobalAdmin(user)} /> : user.memberships.length === 0 ? <LobbyDashboard /> : runsEvents(user) ? <ArrangerDashboard user={user} /> : <WriterDashboard user={user} />}
      {(user.role === "admin" || user.memberships.length > 0) && <Onboarding user={user} />}
    </>
  );
}

/* ---------------- An organisation's dashboard ---------------- */

function ArrangerDashboard({ user }: { user: Whoami }) {
  const t = useT();
  const loc = t.current();
  const [dialog, setDialog] = useState<Omit<NewEventProps, "onClose"> | null>(null);
  const [selling, setSelling] = useState(false);
  const [sellSteps, setSellSteps] = useSellSteps(user.org_id);

  const data = useLoad(async (signal) => {
    const [events, advs, assigns] = await Promise.all([listEvents(undefined, signal), listAdventures(signal), listAssignments(signal)]);
    return { events, advs, assigns };
  }, []);
  // Whether payouts are set up, for a seller's checklist.
  const orgId = user.org_id;
  const author = canAuthor(user);
  const connect = useLoad((s) => (orgId && author ? getConnectStatus(orgId, s).catch(() => null) : Promise.resolve(null)), [orgId, author]);

  const hour = new Date().getHours();
  // Their name, or a username they chose; never the email, as screens are shared.
  const name = user.display_name || (user.username.includes("@") ? "" : user.username);
  const hello = name
    ? hour < 5 ? t.t("Good evening, {{name}}", { name }) : hour < 12 ? t.t("Good morning, {{name}}", { name }) : hour < 18 ? t.t("Good afternoon, {{name}}", { name }) : t.t("Good evening, {{name}}", { name })
    : hour < 5 ? t.t("Good evening") : hour < 12 ? t.t("Good morning") : hour < 18 ? t.t("Good afternoon") : t.t("Good evening");
  const orgName = activeMembership(user)?.org_name;
  const canStart = !!data.data && canStartEvent(data.data.advs, data.data.assigns);

  const header = (
    <PageHeader
      title={hello}
      {...(orgName ? { subtitle: orgName } : {})}
      actions={
        <>
          {canStart && <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setDialog({})}>{t.t("Start a new event")}</Button>}
        </>
      }
    />
  );

  // A failed call must not fall through to the zero-events branch: a
  // transient 5xx would show a veteran the Getting-started checklist.
  if (!data.data) {
    return (
      <Page>
        {header}
        {data.error ? (
          <ErrorBox message={t.t("Couldn't load the dashboard ({{err}}).", { err: data.error })} retry={data.reload} retryLabel={t.t("Retry")} />
        ) : (
          <Loading label={t.t("Loading…")} />
        )}
        {dialog && <NewEventDialog {...dialog} onClose={() => setDialog(null)} />}
      </Page>
    );
  }

  const { events, advs, assigns } = data.data;
  const mine = advs.filter((a) => a.mine_to_edit);
  const unused = assigns.filter((a) => !a.event_id);
  // A paused event is still under way and wants its controls to hand.
  const live = events.filter((r) => r.state === "running" || r.state === "paused");
  const scheduled = events.filter((r) => r.state === "scheduled").sort((a, b) => (a.scheduled_at || "").localeCompare(b.scheduled_at || ""));
  const publisher = !!activeMembership(user)?.is_publisher;
  // A store seller's checklist, until a listing is approved: for an org
  // with a self-guided adventure, or one that asked for these steps.
  const quests = events.filter((r) => r.public_for_sale);
  const canSell = author && !(publisher && CATALOG_ENABLED);
  const seller = canSell && (quests.length > 0 || sellSteps);
  const switchSteps = canSell && quests.length === 0 && (
    <Button size="sm" variant="ghost" onClick={() => setSellSteps(!sellSteps)}>
      {sellSteps ? t.t("Running live events instead? Show those steps") : t.t("Selling tickets in your store? Show those steps")}
    </Button>
  );

  let setup: { lead: string; steps: SetupStepProps[] } | null = null;
  if (seller) {
    if (!quests.some((r) => r.listed || r.listing_review === "approved")) {
      const status = connect.data;
      const fee = !!status?.platform_fee_enabled;
      setup = {
        lead: t.t("Three steps to selling tickets in your store."),
        steps: [
          {
            done: !!status && (!fee || !!status.charges_enabled),
            title: t.t("Set up payouts"),
            hint: fee || !status ? t.t("Connect your Stripe account under Settings → Billing: ticket money is paid out to it.") : t.t("Nothing to connect: tickets are paid to the platform and paid out to you. The terms are under Settings → Billing."),
            action: { label: t.t("Open Billing"), href: href({ name: "settings", tab: "billing" }) },
          },
          { done: quests.length > 0, title: t.t("Put an adventure on sale"), hint: t.t("A published adventure, a name, a price per ticket and a cover. Creating it sends it for review."), action: { label: t.t("New self-guided adventure"), onClick: () => setSelling(true) } },
          { done: false, title: t.t("Get it approved"), hint: t.t("A reviewer checks the listing and its adventure. Once approved, it is in your store."), action: quests.length > 0 ? { label: t.t("Open Self-guided"), href: href({ name: "quests" }) } : null },
        ],
      };
    }
  } else if (events.length === 0) {
    if (publisher && CATALOG_ENABLED) {
      // playable: published from the designer; published: listed in the catalog by the platform admin.
      const playable = mine.some((a) => a.playable);
      const listed = mine.some((a) => a.published);
      setup = {
        lead: t.t("Three steps to a catalog other organizations can run."),
        steps: [
          { done: mine.length > 0, title: t.t("Author an adventure"), hint: t.t("Puzzles, places and story, in the designer."), action: { label: t.t("Create"), href: href({ name: "adventures" }) } },
          { done: playable, title: t.t("Test play and publish it"), hint: t.t("Publish from the designer’s toolbar once it plays through."), action: mine.length > 0 ? { label: t.t("Open your adventures"), href: href({ name: "adventures" }) } : null },
          { done: listed, title: t.t("Get it listed in the catalog"), hint: t.t("The platform admin lists published adventures for other organizations to run.") },
        ],
      };
    } else {
      setup = {
        lead: t.t("Two steps from here to your first event."),
        steps: [
          canAuthor(user)
            ? { done: mine.length > 0 || unused.length > 0, title: t.t("Pick an adventure"), hint: CATALOG_ENABLED ? t.t("Browse the catalog or write your own.") : t.t("Puzzles, places and story, in the designer."), action: { label: t.t("Create"), href: href({ name: "adventures" }) } }
            : { done: advs.length > 0 || unused.length > 0, title: t.t("Pick an adventure"), hint: t.t("Your organization's admins write the adventures; you run events with them."), action: { label: t.t("Open adventures"), href: href({ name: "adventures" }) } },
          { done: false, title: t.t("Schedule and start"), hint: t.t("Pick a date, add teams, share each player’s magic link. Each team is charged as it starts."), action: canStart ? { label: t.t("Schedule"), onClick: () => setDialog({}) } : null },
        ],
      };
    }
  }

  const next = scheduled.find((r) => r.scheduled_at);
  const playing = live.reduce((n, r) => n + r.teams_count, 0);

  return (
    <Page>
      {header}
      <div className="db-tiles">
        <Tile
          icon={<Radio size={20} />}
          value={live.length}
          label={t.t("Live now")}
          hint={live.length > 0 ? t.plural(playing, { one: t.t("{{n}} team playing"), other: t.t("{{n}} teams playing") }) : t.t("Nothing running")}
          {...(live.length > 0 ? { tone: "live" as const } : {})}
        />
        <Tile
          icon={<CalendarClock size={20} />}
          value={scheduled.length}
          label={t.t("Scheduled")}
          hint={next ? t.t("Next {{when}}", { when: relTime(next.scheduled_at, loc) }) : scheduled.length > 0 ? t.t("No start time set") : t.t("Nothing booked")}
          href={href({ name: "events" })}
        />
        {unused.length > 0 && (
          <Tile
            icon={<Ticket size={20} />}
            value={unused.length}
            label={t.t("Unused event passes")}
            hint={t.t("Ready to schedule")}
            href={href({ name: "settings", tab: "purchases" })}
          />
        )}
      </div>

      {setup && <SetupCard {...setup} foot={switchSteps} />}
      {!setup && unused.length > 0 && (
        <Callout variant="info">
          <Row>
            <span>{t.plural(unused.length, { one: t.t("{{n}} event pass is paid and ready: schedule its event."), other: t.t("{{n}} event passes are paid and ready: schedule their events.") })}</span>
            <Button size="sm" variant="primary" onClick={() => setDialog({ assignmentId: unused[0]!.id })}>{t.t("Schedule")}</Button>
          </Row>
        </Callout>
      )}

      {live.length > 0 && (
        <section className="db-section" aria-labelledby="db-live-h">
          <h2 id="db-live-h" className="db-section__title">{t.t("Live now")}</h2>
          <div className="db-live-grid">
            {live.map((r) => <LiveCard key={r.id} event={r} />)}
          </div>
        </section>
      )}

      {scheduled.length > 0 && (
        <section className="db-section" aria-labelledby="db-soon-h">
          <div className="db-section__head">
            <h2 id="db-soon-h" className="db-section__title">{t.t("Starts soon")}</h2>
            {scheduled.length > 5 && <a className="db-section__more" href={href({ name: "events" })}>{t.t("All {{n}} scheduled", { n: scheduled.length })}<ArrowRight size={14} aria-hidden /></a>}
          </div>
          <Agenda events={scheduled.slice(0, 5)} onStarted={data.reload} />
        </section>
      )}

      {live.length === 0 && scheduled.length === 0 && events.length > 0 && (
        <Card title={t.t("Recent events")} flush>
          <EventsTable rows={events.slice(0, 5)} />
        </Card>
      )}

      {dialog && <NewEventDialog {...dialog} onClose={() => setDialog(null)} />}
      {selling && <NewQuestDialog onClose={() => setSelling(false)} onCreated={data.reload} />}
    </Page>
  );
}

/** Whether the checklist shows a seller's steps, remembered per organisation on this browser. */
function useSellSteps(orgId: number | undefined): [boolean, (on: boolean) => void] {
  const key = "zv-sell-steps-" + (orgId ?? 0);
  const [on, setOn] = useState(() => {
    try {
      return localStorage.getItem(key) === "1";
    } catch {
      return false;
    }
  });
  const set = (next: boolean) => {
    setOn(next);
    try {
      if (next) localStorage.setItem(key, "1");
      else localStorage.removeItem(key);
    } catch {
      /* storage blocked: it holds for this visit */
    }
  };
  return [on, set];
}

/* ---------------- A writer's: the adventures they author ---------------- */

function WriterDashboard({ user }: { user: Whoami }) {
  const t = useT();
  const data = useLoad((signal) => listAdventures(signal), []);
  const orgName = activeMembership(user)?.org_name;
  const mine = (data.data ?? []).filter((a) => a.mine_to_edit);
  const columns: Column<(typeof mine)[number]>[] = [
    { key: "title", header: t.t("Adventure"), wide: true, cell: (a) => a.title || a.slug },
  ];
  return (
    <Page>
      <PageHeader
        title={t.t("Hello, {{name}}", { name: user.username })}
        {...(orgName ? { subtitle: orgName } : {})}
        actions={<a className="btn primary" href={href({ name: "adventures" })}>{t.t("Open adventures")}</a>}
      />
      {data.error && !data.data ? (
        <ErrorBox message={t.t("Couldn't load the dashboard ({{err}}).", { err: data.error })} retry={data.reload} retryLabel={t.t("Retry")} />
      ) : !data.data ? (
        <Loading label={t.t("Loading…")} />
      ) : mine.length === 0 ? (
        <Callout variant="info">{t.t("As a writer you create and edit your organization's adventures. Start one under Adventures.")}</Callout>
      ) : (
        <Card title={t.t("Your organization's adventures")} flush>
          <Table columns={columns} rows={mine} rowKey={(a) => a.slug} rowHref={(a) => href({ name: "adventure", slug: a.slug })} caption={t.t("Your organization's adventures")} />
        </Card>
      )}
    </Page>
  );
}

/* ---------------- The lobby: signed in, in no organisation ---------------- */

function LobbyDashboard() {
  const t = useT();
  const { state } = useSession();
  const email = state.status === "authenticated" ? state.user.email || state.user.username : "";
  const [creating, setCreating] = useState(false);
  return (
    <Page>
      <PageHeader title={t.t("Welcome to {{name}}", { name: siteName() })} />
      <Card>
        <div className="db-lobby">
          <img className="db-lobby__art" src={WIDE.noOrganisation.small} srcSet={wideSrcSet(WIDE.noOrganisation)} sizes="(min-width: 40rem) 28rem, 100vw" alt="" width={800} height={609} />
          <div className="db-lobby__text">
            <p>{t.t("You're signed in but not part of any organization yet. Either create one (you'll be its admin), or ask an admin of an existing organization to add you under Settings → Members with your email, {{email}}.", { email })}</p>
            <div>
              <Button variant="primary" icon={<Building2 size={16} aria-hidden />} onClick={() => setCreating(true)}>{t.t("Create an organization")}</Button>
            </div>
          </div>
        </div>
      </Card>
      {creating && <CreateOrgDialog own onClose={() => setCreating(false)} />}
    </Page>
  );
}

/**
 * A new organisation: the platform admin's tenant, or (own) the user's
 * own, which they become the admin of and the session moves to.
 */
function CreateOrgDialog({ own, onClose, onCreated }: { own?: boolean; onClose: () => void; onCreated?: () => void }) {
  const t = useT();
  const toast = useToast();
  const { refresh } = useSession();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit() {
    const n = name.trim();
    if (!n) return setError(t.t("Name required"));
    setBusy(true);
    setError(null);
    try {
      if (own) {
        await createOwnOrganization(n);
        onClose();
        await refresh();
        navigate({ name: "dashboard" });
        toast.ok(t.t("Organization \"{{name}}\" created — you're its admin", { name: n }));
      } else {
        const o = await createOrganization(n);
        toast.ok(t.t("Organization created"));
        onClose();
        // Adding its first user is the next step, on its page.
        if (o?.id) navigate({ name: "organization", id: o.id });
        else onCreated?.();
      }
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  }

  return (
    <Dialog
      title={own ? t.t("Create an organization") : t.t("New organization")}
      onClose={() => !busy && onClose()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field
        label={own ? t.t("Organization name") : t.t("Name")}
        hint={own ? t.t("You'll be the only member to start with — invite teammates afterwards from Settings.") : undefined}
        error={error}
      >
        <input autoFocus required value={name} onChange={(e) => setName(e.target.value)} placeholder={own ? t.t("e.g. \"Acme Field Trips\"") : undefined} />
      </Field>
    </Dialog>
  );
}

/* ---------------- The platform admin's dashboard ---------------- */

/** `full` is false for Support, who sees the overview but creates nothing. */
function AdminDashboard({ full }: { full: boolean }) {
  const t = useT();
  const [creating, setCreating] = useState(false);
  const data = useLoad(async (signal) => {
    const [orgs, catalog, server, toInvoice] = await Promise.all([listOrganizations(signal), getCatalog(signal), get<{ signup_enabled: boolean }>("/api/admin/settings", { signal }), listToInvoice(signal)]);
    return { orgs, catalog, signup: server.signup_enabled, toInvoice };
  }, []);

  if (!data.data) {
    return (
      <Page>
        <PageHeader title={t.t("Dashboard")} />
        {data.error ? <ErrorBox message={t.t("Couldn't load the dashboard ({{err}}).", { err: data.error })} retry={data.reload} retryLabel={t.t("Retry")} /> : <Loading label={t.t("Loading…")} />}
      </Page>
    );
  }

  const { orgs, catalog, signup, toInvoice } = data.data;
  const members = (o: OrgRow) => (o.members ?? []).length;
  const totals = orgs.reduce(
    (a, o) => ({ events: a.events + (o.events_total || 0), running: a.running + (o.events_running || 0), members: a.members + members(o) }),
    { events: 0, running: 0, members: 0 },
  );

  const publisher = orgs.find((o) => o.is_publisher);
  const players = orgs.reduce((n, o) => n + (o.players_total || 0), 0);
  const live = orgs.filter((o) => o.events_running > 0).length;

  // Past bootstrap once any organisation has held an event.
  let setup: SetupStepProps[] | null = null;
  if (full && totals.events === 0) {
    const haveOrg = orgs.length > 0;
    const manage = { label: t.t("Manage"), href: href({ name: "organizations" }) };
    setup = [
      { done: haveOrg, title: t.t("Create an organization"), hint: t.t("One organization per customer. They’ll run their own events inside it."), action: { label: t.t("Create"), onClick: () => setCreating(true) } },
      { done: orgs.some((o) => members(o) > 0), title: t.t("Invite the first user"), hint: t.t("They’ll set up events and share player links for that organization."), action: haveOrg ? manage : null },
      ...(CATALOG_ENABLED ? [{
        done: !!publisher && (catalog.adventures ?? []).some((a) => a.published),
        title: t.t("Add an adventure to the catalog"),
        hint: t.t("Make one organization the catalog publisher; list the adventures it authors so orgs can pick from them."),
        action: haveOrg ? { label: t.t("Manage"), href: publisher ? href({ name: "organization", id: publisher.id }) : href({ name: "organizations" }) } : null,
      }] : []),
      { done: signup, title: t.t("Turn on public sign-up"), hint: t.t("Off on a new server, so only you can create accounts. Turn it on when anyone may sign up and create an organization."), action: { label: t.t("Open settings"), href: href({ name: "settings", tab: "system" }) } },
    ];
  }

  const columns: Column<OrgRow>[] = [
    { key: "name", header: t.t("Organization"), cell: (o) => (
      <span className="db-org">
        <Avatar name={o.name} src={o.has_logo ? `/org-logo/${o.id}` : undefined} size={24} />
        <span className="db-ellipsis">{o.name}</span>
      </span>
    ) },
    { key: "users", header: t.t("Users"), wide: true, align: "end", cell: (o) => members(o) },
    { key: "players", header: t.t("Players"), wide: true, align: "end", cell: (o) => o.players_total || 0 },
    { key: "events", header: t.t("Events"), align: "end", cell: (o) => o.events_total || 0 },
    { key: "active", header: t.t("Active"), cell: (o) => (o.events_running > 0 ? <Pill tone="good">{t.t("{{n}} running", { n: o.events_running })}</Pill> : <span className="db-muted">—</span>) },
  ];

  const invoiceColumns: Column<PurchaseRow>[] = [
    { key: "org", header: t.t("Organization"), cell: (p) => <span className="db-ellipsis">{p.org_name || "—"}</span> },
    { key: "adventure", header: t.t("Adventure"), wide: true, cell: (p) => <span className="db-ellipsis">{p.adventure_title || p.adventure_slug}</span> },
    { key: "players", header: t.t("Players"), wide: true, align: "end", cell: (p) => p.max_players },
    { key: "total", header: t.t("Total"), align: "end", cell: (p) => fmtMoney(p.amount_cents, p.currency, t.current()) },
    { key: "when", header: t.t("Bought"), cell: (p) => relTime(p.paid_at || p.created_at, t.current()) },
  ];

  return (
    <Page>
      <PageHeader
        title={t.t("Dashboard")}
        {...(full && orgs.length > 0 ? { actions: <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New organization")}</Button> } : {})}
      />
      <div className="db-tiles">
        <Tile
          icon={<Radio size={20} />}
          value={totals.running}
          label={t.t("Live now")}
          hint={totals.running > 0 ? t.plural(live, { one: t.t("in {{n}} organization"), other: t.t("in {{n}} organizations") }) : t.plural(totals.events, { one: t.t("{{n}} event in all"), other: t.t("{{n}} events in all") })}
          {...(totals.running > 0 ? { tone: "live" as const } : {})}
        />
        <Tile
          icon={<Building2 size={20} />}
          value={orgs.length}
          label={t.t("Organizations")}
          {...(CATALOG_ENABLED ? { hint: publisher ? t.t("Catalog by {{name}}", { name: publisher.name }) : t.t("No catalog publisher yet") } : {})}
          href={href({ name: "organizations" })}
        />
        <Tile
          icon={<Users size={20} />}
          value={totals.members}
          label={t.t("Org users")}
          hint={t.plural(players, { one: t.t("{{n}} player so far"), other: t.t("{{n}} players so far") })}
        />
      </div>
      {setup && <SetupCard lead={setup.length === 4 ? t.t("Four steps to a working deployment.") : t.t("Three steps to a working deployment.")} steps={setup} />}
      {toInvoice.length > 0 && (
        <section className="db-section" aria-labelledby="db-invoice-h">
          <div className="db-section__head">
            <h2 id="db-invoice-h" className="db-section__title">{t.t("To invoice")}</h2>
            {full && <a className="db-section__more" href={href({ name: "settings", tab: "purchases" })}>{t.t("Open purchases")}<ArrowRight size={14} aria-hidden /></a>}
          </div>
          <Card flush>
            <Table columns={invoiceColumns} rows={toInvoice} rowKey={(p) => p.id} rowHref={(p) => href({ name: "organization", id: p.org_id })} caption={t.t("To invoice")} />
          </Card>
        </section>
      )}
      {orgs.length > 0 && (
        <section className="db-section" aria-labelledby="db-orgs-h">
          <div className="db-section__head">
            <h2 id="db-orgs-h" className="db-section__title">{t.t("Organizations")}</h2>
            <a className="db-section__more" href={href({ name: "organizations" })}>{t.t("Manage organizations")}<ArrowRight size={14} aria-hidden /></a>
          </div>
          <Card flush>
            <Table columns={columns} rows={orgs} rowKey={(o) => o.id} rowHref={(o) => href({ name: "organization", id: o.id })} caption={t.t("Organizations")} />
          </Card>
        </section>
      )}
      {creating && <CreateOrgDialog onClose={() => setCreating(false)} onCreated={data.reload} />}
    </Page>
  );
}
