import { useState } from "react";
import { ArrowRight, Building2, CalendarClock, Plus, Radio, Ticket, Users } from "lucide-react";
import { siteName, type Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { CATALOG_ENABLED, href, navigate } from "../../router.ts";
import { Avatar, Button, Card, Dialog, ErrorBox, Field, Loading, Page, PageHeader, Pill, Table, errorText, relTime, useLoad, useToast, type Column } from "../../ui/index.ts";
import { activeMembership, listAdventures, listAssignments, listEvents } from "../events/api.ts";
import { EventsTable } from "../events/parts.tsx";
import { NewEventDialog, canStartEvent, type NewEventProps } from "../events/NewEventDialog.tsx";
import { useJustPurchased } from "../events/justPurchased.ts";
import { createOrganization, createOwnOrganization, getCatalog, listOrganizations, type OrgRow } from "./api.ts";
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
      {user.role === "admin" ? <AdminDashboard /> : user.memberships.length === 0 ? <LobbyDashboard /> : <ArrangerDashboard user={user} />}
      {(user.role === "admin" || user.memberships.length > 0) && <Onboarding user={user} />}
    </>
  );
}

/* ---------------- An organisation's dashboard ---------------- */

function ArrangerDashboard({ user }: { user: Whoami }) {
  const t = useT();
  const loc = t.current();
  const [dialog, setDialog] = useState<Omit<NewEventProps, "onClose"> | null>(null);
  useJustPurchased((assignmentId) => setDialog({ assignmentId }));

  const data = useLoad(async (signal) => {
    const [events, advs, assigns] = await Promise.all([listEvents(undefined, signal), listAdventures(signal), listAssignments(signal)]);
    return { events, advs, assigns };
  }, []);

  const hour = new Date().getHours();
  const hello = hour < 5 ? t.t("Good evening, {{name}}", { name: user.username }) : hour < 12 ? t.t("Good morning, {{name}}", { name: user.username }) : hour < 18 ? t.t("Good afternoon, {{name}}", { name: user.username }) : t.t("Good evening, {{name}}", { name: user.username });
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

  let setup: { lead: string; steps: SetupStepProps[] } | null = null;
  if (events.length === 0) {
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
        lead: t.t("Three steps from here to your first event."),
        steps: [
          { done: mine.length > 0 || unused.length > 0, title: t.t("Pick an adventure"), hint: CATALOG_ENABLED ? t.t("Browse the catalog or write your own.") : t.t("Puzzles, places and story, in the designer."), action: { label: t.t("Create"), href: href({ name: "adventures" }) } },
          { done: unused.length > 0, title: t.t("Get an event pass"), hint: t.t("One pass per event, sized by players. The first 15 players are free."), action: CATALOG_ENABLED ? { label: t.t("Open the catalog"), href: href({ name: "catalog" }) } : { label: t.t("Open your adventures"), href: href({ name: "adventures" }) } },
          { done: false, title: t.t("Schedule and start"), hint: t.t("Pick a date, add teams, share each player’s magic link."), action: unused.length > 0 ? { label: t.t("Schedule"), onClick: () => setDialog({ assignmentId: unused[0]!.id }) } : null },
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
          hint={next ? t.t("Next {{when}}", { when: relTime(next.scheduled_at, loc) }) : t.t("Nothing booked")}
          href={href({ name: "events" })}
        />
        <Tile
          icon={<Ticket size={20} />}
          value={unused.length}
          label={t.t("Unused event passes")}
          hint={unused.length > 0 ? t.t("Ready to schedule") : CATALOG_ENABLED ? t.t("Get one in the catalog") : t.t("Buy an event pass")}
          href={unused.length > 0 ? href({ name: "settings", tab: "purchases" }) : href({ name: CATALOG_ENABLED ? "catalog" : "adventures" })}
        />
      </div>

      {setup && <SetupCard {...setup} />}

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
    </Page>
  );
}

/* ---------------- The lobby: signed in, in no organisation ---------------- */

function LobbyDashboard() {
  const t = useT();
  const [creating, setCreating] = useState(false);
  return (
    <Page>
      <PageHeader title={t.t("Welcome to {{name}}", { name: siteName() })} />
      <Card>
        <p>{t.t("You're signed in but not part of any organization yet. Either create one (you'll be its admin) or ask someone in an existing organization to invite you.")}</p>
        <div>
          <Button variant="primary" icon={<Building2 size={16} aria-hidden />} onClick={() => setCreating(true)}>{t.t("Create an organization")}</Button>
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
        await createOrganization(n);
        toast.ok(t.t("Organization created"));
        onClose();
        onCreated?.();
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

function AdminDashboard() {
  const t = useT();
  const [creating, setCreating] = useState(false);
  const data = useLoad(async (signal) => {
    const [orgs, catalog] = await Promise.all([listOrganizations(signal), getCatalog(signal)]);
    return { orgs, catalog };
  }, []);

  if (!data.data) {
    return (
      <Page>
        <PageHeader title={t.t("Dashboard")} />
        {data.error ? <ErrorBox message={t.t("Couldn't load the dashboard ({{err}}).", { err: data.error })} retry={data.reload} retryLabel={t.t("Retry")} /> : <Loading label={t.t("Loading…")} />}
      </Page>
    );
  }

  const { orgs, catalog } = data.data;
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
  if (totals.events === 0) {
    const haveOrg = orgs.length > 0;
    const manage = { label: t.t("Manage"), href: href({ name: "organizations" }) };
    setup = [
      { done: haveOrg, title: t.t("Create an organization"), hint: t.t("One organization per customer. They’ll run their own events inside it."), action: { label: t.t("Create"), onClick: () => setCreating(true) } },
      { done: orgs.some((o) => members(o) > 0), title: t.t("Invite the first user"), hint: t.t("They’ll set up events and share player links for that organization."), action: haveOrg ? manage : null },
      {
        done: !!publisher && (catalog.adventures ?? []).some((a) => a.published),
        title: t.t("Add an adventure to the catalog"),
        hint: t.t("Make one organization the catalog publisher; list the adventures it authors so orgs can pick from them."),
        action: haveOrg ? { label: t.t("Manage"), href: publisher ? href({ name: "organization", id: publisher.id }) : href({ name: "organizations" }) } : null,
      },
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

  return (
    <Page>
      <PageHeader
        title={t.t("Dashboard")}
        {...(orgs.length > 0 ? { actions: <Button variant="primary" icon={<Plus size={16} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New organization")}</Button> } : {})}
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
          hint={publisher ? t.t("Catalog by {{name}}", { name: publisher.name }) : t.t("No catalog publisher yet")}
          href={href({ name: "organizations" })}
        />
        <Tile
          icon={<Users size={20} />}
          value={totals.members}
          label={t.t("Org users")}
          hint={t.plural(players, { one: t.t("{{n}} player so far"), other: t.t("{{n}} players so far") })}
        />
      </div>
      {setup && <SetupCard lead={t.t("Three steps to a working deployment.")} steps={setup} />}
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
