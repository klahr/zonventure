import { useEffect, useRef, useState, type ReactNode } from "react";
import { Ban, Coins, ExternalLink, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { useTopBarTitle } from "../../topbar.tsx";
import {
  Button, Card, ErrorBox, HelpPopover, ICON, ImageDrop, Loading, Page, PageHeader, Pill, Table, fmtDateTime, fmtMoney, relTime, useDialogs, useLoad, useToast,
  type Column, type Tone,
} from "../../ui/index.ts";
import {
  cancelOrgEvent, connectStatus, deleteLogo, deleteOrganization, getOrganization, getPurchase, grantShare, listCatalog, listOrganizations,
  listOrgEvents, listPurchases, refreshConnect, refundPurchase, removeMember, revokeShare, setBilled, setPartyAdventures, setPublished, setPublisher, setTrusted, setVerifiedCreator,
  uploadLogo, verifyEmail, voidPurchase,
  type Assignment, type CatalogAdventure, type ConnectStatus, type OrgEvent, type OrgMember, type Organization, type Purchase,
} from "./api.ts";
import { AddMemberDialog, EditPassDialog, GrantPassDialog, GrantSlotsDialog, PickDialog, RenameOrgDialog } from "./OrgDialogs.tsx";
import { OrgLogo } from "./OrgLogo.tsx";
import { MoreMenu, type MenuItem } from "../event/Menu.tsx";
import "./admin.css";

interface Data {
  org: Organization;
  adventures: CatalogAdventure[];
  events: OrgEvent[];
  purchases: Purchase[];
  connect: ConnectStatus | null;
}

type Open =
  | { kind: "rename" }
  | { kind: "add-member" }
  | { kind: "grant-pass" }
  | { kind: "edit-pass"; pass: Assignment }
  | { kind: "grant-slots"; event: OrgEvent }
  | { kind: "commission"; candidates: CatalogAdventure[] }
  | { kind: "share"; adventure: CatalogAdventure; orgs: Organization[] };

const STATE_TONE: Record<string, Tone> = { running: "good", scheduled: "accent", paused: "warn", completed: "good", cancelled: "bad" };
const live = (e: OrgEvent) => e.state === "scheduled" || e.state === "running" || e.state === "paused";

/**
 * One organisation, for the platform admin: its setup (logo, invoice
 * billing, catalog publishing), people, events, catalog access, event
 * passes and purchases, and deleting it. Running its events is the
 * organisation's own job; the operator only adds players and cancels.
 */
export function OrganizationPage({ id }: { id: number }) {
  const t = useT();
  const load = useLoad<Data>(async (signal) => {
    const soft = <T,>(p: Promise<T>, fallback: T) => p.catch((e: unknown) => { if (signal.aborted) throw e; return fallback; });
    const [org, adventures, purchases, events, connect] = await Promise.all([
      getOrganization(id, signal),
      soft(listCatalog(signal), []),
      soft(listPurchases(signal), []),
      soft(listOrgEvents(id, signal), []),
      soft(connectStatus(id, signal), null),
    ]);
    return { org, adventures, events, connect, purchases: purchases.filter((p) => p.org_id === id) };
  }, [id]);
  useTopBarTitle(load.data?.org.name ?? null);

  if (load.error && !load.data) {
    return (
      <Page>
        <PageHeader title={t.t("Organizations")} back={{ href: href({ name: "organizations" }), label: t.t("Organizations") }} />
        <ErrorBox message={t.t("Couldn't load this organization ({{err}}).", { err: load.error })} retry={load.reload} retryLabel={t.t("Retry")} />
      </Page>
    );
  }
  if (!load.data) return <Page><Loading label={t.t("Loading…")} /></Page>;
  return <OrgView data={load.data} reload={load.reload} />;
}

function OrgView({ data, reload }: { data: Data; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { org, adventures, events, purchases } = data;
  const [open, setOpen] = useState<Open | null>(null);
  const close = () => setOpen(null);
  const done = () => { setOpen(null); reload(); };

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    try {
      await fn();
      toast.ok(ok);
      reload();
      return true;
    } catch (e) {
      toast.err(e);
      return false;
    }
  };

  const togglePublisher = async () => {
    const next = !org.is_publisher;
    let password: string | undefined;
    if (!next) {
      const p = await dialogs.password({
        title: t.t("Stop publishing the catalog?"),
        message: t.t("Every other organization loses access to this organization’s catalog adventures until you make it the publisher again. Enter your password to confirm."),
        confirmLabel: t.t("Stop publishing"),
        danger: true,
      });
      if (!p) return;
      password = p;
    }
    await act(() => setPublisher(org.id, next, password), next ? t.t("Now the catalog publisher") : t.t("No longer the catalog publisher"));
  };

  const removeUser = async (m: OrgMember) => {
    if (m.active_events_owned > 0) {
      const go = await dialogs.confirm({
        title: t.t("Remove member with active events?"),
        message: (
          <>
            {t.t("{{username}} created {{n}} active event(s) in {{org}}.", { username: m.username, n: m.active_events_owned, org: org.name })}{" "}
            {t.t("Those events will keep running but lose their original creator from the audit trail. Consider cancelling or finishing them first, or transferring ownership of the org to a different admin who can manage them after removal.")}
          </>
        ),
        confirmLabel: t.t("Remove anyway"),
        danger: true,
      });
      if (!go) return;
    }
    const p = await dialogs.password({
      title: t.t("Remove {{username}}?", { username: m.username }),
      message: t.t("Removes {{username}} from {{org}}. Enter your password to confirm.", { username: m.username, org: org.name }),
      confirmLabel: t.t("Remove"),
      danger: true,
    });
    if (!p) return;
    await act(() => removeMember(org.id, m.id, p), t.t("Removed"));
  };

  const verify = async (m: OrgMember) => {
    const go = await dialogs.confirm({
      title: t.t("Verify email"),
      message: t.t("Manually verify {{username}}'s email? Only do this after confirming the user out-of-band — this bypasses the verification mail loop.", { username: m.username }),
      confirmLabel: t.t("Verify email"),
    });
    if (go) await act(() => verifyEmail(m.id), t.t("Email verified"));
  };

  const cancelEvent = async (e: OrgEvent) => {
    const p = await dialogs.password({
      title: t.t("Cancel event?"),
      message: e.state === "scheduled"
        ? t.t("The event will be marked cancelled and never start. Teams already invited keep their links but get a \"cancelled\" screen if they open the app. Enter your password to confirm.")
        : t.t("The event ends now. Live pings stop, the leaderboard freezes, and teams get a \"cancelled\" screen on their next refresh. Enter your password to confirm."),
      cancelLabel: t.t("Keep event"),
      confirmLabel: t.t("Cancel event"),
      danger: true,
    });
    if (p) await act(() => cancelOrgEvent(org.id, e.id, p), t.t("Event cancelled"));
  };

  const revoke = async (a: CatalogAdventure, orgId: number, orgName: string) => {
    const go = await dialogs.confirm({
      title: t.t("Revoke share"),
      message: t.t("Revoke share with {{org}}? Existing events keep working; new events will be blocked.", { org: orgName }),
      confirmLabel: t.t("Revoke"),
      danger: true,
    });
    if (go) await act(() => revokeShare(a.slug, orgId), t.t("Share revoked"));
  };

  const openShare = async (a: CatalogAdventure) => {
    try {
      const all = await listOrganizations();
      const already = new Set((a.shares ?? []).map((s) => s.org_id));
      const orgs = all.filter((o) => !already.has(o.id) && !o.is_publisher);
      if (orgs.length === 0) { toast.err(t.t("Every organization already has access.")); return; }
      setOpen({ kind: "share", adventure: a, orgs });
    } catch (e) {
      toast.err(e);
    }
  };

  const deleteOrg = async () => {
    if (org.events_running > 0) {
      toast.err(t.t("Cannot delete: {{n}} event(s) currently running. Stop them first.", { n: org.events_running }));
      return;
    }
    const summary = t.t("You are about to delete: {{members}} user(s), {{assignments}} event pass(es), {{players}} player(s), {{events}} event(s). This happens immediately and cannot be undone.", {
      members: org.members.length, assignments: org.assignments.length, players: org.players_total, events: org.events_total,
    });
    const p = await dialogs.password({
      title: t.t("Delete organization “{{name}}”?", { name: org.name }),
      message: summary + " " + t.t("Enter your password to confirm."),
      confirmLabel: t.t("Delete organization"),
      danger: true,
    });
    if (!p) return;
    try {
      await deleteOrganization(org.id, p);
      toast.ok(t.t("Organization deleted"));
      navigate({ name: "organizations" });
    } catch (e) {
      toast.err(e);
    }
  };

  const commissioned = adventures.filter((a) => (a.shares ?? []).some((s) => s.org_id === org.id));
  const candidates = adventures.filter((a) => !commissioned.includes(a));
  const passOf = new Map(org.assignments.map((a) => [a.id, a]));
  const shownEvents = new Set(events.map((e) => e.id));
  const more: MenuItem[] = [
    ...(org.slug ? [{ label: t.t("Open public page"), icon: <ExternalLink size={ICON.sm} aria-hidden />, href: `/o/${encodeURIComponent(org.slug)}`, target: "_blank" }, { separator: true as const }] : []),
    {
      label: t.t("Delete organization"),
      icon: <Trash2 size={ICON.sm} aria-hidden />,
      danger: true,
      title: t.t("Deleting removes every user, event pass, and event in this organization. The action is not reversible."),
      onClick: () => void deleteOrg(),
    },
  ];

  return (
    <Page>
      <PageHeader
        back={{ href: href({ name: "organizations" }), label: t.t("Organizations") }}
        title={
          <>
            {org.has_logo && <OrgLogo org={org} size={36} />}
            {org.name}
            <Button variant="ghost" size="icon" aria-label={t.t("Rename")} title={t.t("Rename")} icon={<Pencil size={ICON.md} aria-hidden />} onClick={() => setOpen({ kind: "rename" })} />
          </>
        }
        badges={
          <>
            {org.events_running > 0 && <Pill tone="good">{t.t("{{n}} running", { n: org.events_running })}</Pill>}
            {org.trusted && <Pill tone="good">{t.t("Invoice billing")}</Pill>}
            {org.verified_creator && <Pill tone="good">{t.t("Verified creator")}</Pill>}
            {org.party_adventures && <Pill tone="good">{t.t("Party adventures")}</Pill>}
            {org.is_publisher && <Pill tone="accent">{t.t("Catalog publisher")}</Pill>}
          </>
        }
        subtitle={
          <>
            {t.plural(org.players_total, { one: t.t("{{n}} player"), other: t.t("{{n}} players") })}
            {" · "}
            {t.plural(org.events_total, { one: t.t("{{n}} event"), other: t.t("{{n}} events") })}
            {" · "}
            <span title={fmtDateTime(org.created_at, t.current())}>{t.t("created {{when}}", { when: relTime(org.created_at, t.current()) })}</span>
          </>
        }
        actions={<MoreMenu label={t.t("More")} items={more} />}
      />

      <SetupCard org={org} connect={data.connect} onTrusted={() => act(() => setTrusted(org.id, !org.trusted), org.trusted ? t.t("Invoice billing disabled") : t.t("Invoice billing enabled"))} onParty={() => act(() => setPartyAdventures(org.id, !org.party_adventures), org.party_adventures ? t.t("Party adventures disabled") : t.t("Party adventures enabled"))} onVerified={() => act(() => setVerifiedCreator(org.id, !org.verified_creator), org.verified_creator ? t.t("No longer a verified creator") : t.t("Verified creator: their waiting listings are approved"))} onPublisher={togglePublisher} reload={reload} />

      <Card
        title={t.t("Users")}
        actions={<Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => setOpen({ kind: "add-member" })}>{t.t("Add user")}</Button>}
        flush={org.members.length > 0}
      >
        <Table<OrgMember>
          rows={org.members}
          rowKey={(m) => m.id}
          caption={t.t("Users")}
          empty={<p className="adm-muted">{t.t("No users yet — add one to get started.")}</p>}
          columns={[
            { key: "name", header: t.t("Username"), cell: (m) => <strong>{m.username}</strong> },
            { key: "role", header: t.t("Role"), cell: (m) => (m.role === "org_admin" ? t.t("Admin") : t.t("Event host")) },
            {
              key: "email", header: t.t("Email"), cell: (m) => m.email_verified
                ? <span className="adm-sub">{t.t("Verified")}</span>
                : (
                  <span className="adm-inline">
                    <Pill tone="warn">{t.t("Unverified")}</Pill>
                    <Button size="sm" variant="ghost" title={t.t("Manually verify email (use only after confirming the user out-of-band)")} onClick={() => verify(m)}>{t.t("verify")}</Button>
                  </span>
                ),
            },
            {
              key: "x", header: <span className="cs-sr">{t.t("Actions")}</span>, align: "end", cell: (m) => (
                <Button size="icon" variant="ghost" className="danger" icon={<Trash2 size={ICON.sm} aria-hidden />} aria-label={`${t.t("Remove")} ${m.username}`} title={t.t("Remove")} onClick={() => removeUser(m)} />
              ),
            },
          ]}
        />
      </Card>

      <Card title={t.t("Events")} flush={events.length > 0}>
        <Table<OrgEvent>
          rows={events}
          rowKey={(e) => e.id}
          caption={t.t("Events")}
          empty={<p className="adm-muted">{t.t("No events yet.")}</p>}
          columns={eventColumns(t, {
            add: (e) => setOpen({ kind: "grant-slots", event: e }),
            cancel: cancelEvent,
            pass: (e) => passOf.get(e.assignment_id),
            editPass: (pass) => setOpen({ kind: "edit-pass", pass }),
          })}
        />
      </Card>

      {org.is_publisher ? (
        <Card
          title={
            <span className="adm-inline">
              {t.t("Catalog")}
              <HelpPopover label={t.t("About the catalog")} body={t.t("This organization authors the catalog. Listed adventures are open to every organization; commission one to give a specific organization free access without listing it.")} />
            </span>
          }
          subtitle={t.t("{{n}} of {{total}} listed", { n: adventures.filter((a) => a.published).length, total: adventures.length })}
          flush={adventures.length > 0}
        >
          <Table<CatalogAdventure>
            rows={adventures}
            rowKey={(a) => a.slug}
            caption={t.t("Catalog")}
            empty={<p className="adm-muted">{t.t("No adventures yet — this organization’s authors create them.")}</p>}
            columns={[
              {
                key: "title", header: t.t("Adventure"), cell: (a) => (
                  <>
                    <div className="adm-inline">{a.title}{a.published && <Pill tone="good">{t.t("Listed")}</Pill>}</div>
                    <div className="adm-sub adm-mono">{a.slug}</div>
                    {(a.shares ?? []).length > 0 && (
                      <div className="adm-chips adm-shares">
                        <span className="adm-sub">{t.t("Commissioned for")}</span>
                        {(a.shares ?? []).map((s) => (
                          <Chip key={s.org_id} onRemove={() => revoke(a, s.org_id, s.org_name)} removeLabel={t.t("Revoke share")}>{s.org_name}</Chip>
                        ))}
                      </div>
                    )}
                  </>
                ),
              },
              {
                key: "x", header: <span className="cs-sr">{t.t("Actions")}</span>, align: "end", cell: (a) => (
                  <span className="adm-actions">
                    <Button size="sm" onClick={() => act(() => setPublished(a.slug, !a.published), a.published ? t.t("Removed from the catalog") : t.t("Listed in the catalog"))}>
                      {a.published ? t.t("Unlist") : t.t("List in catalog")}
                    </Button>
                    <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => openShare(a)}>{t.t("Commission")}</Button>
                  </span>
                ),
              },
            ]}
          />
        </Card>
      ) : (
        <Card
          title={t.t("Commissioned adventures")}
          actions={candidates.length > 0 && <Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={() => setOpen({ kind: "commission", candidates })}>{t.t("Commission adventure")}</Button>}
        >
          {commissioned.length === 0 ? (
            <p className="adm-muted">{t.t("None — commission a catalog adventure to let them run it without buying event passes.")}</p>
          ) : (
            <div className="adm-chips">
              {commissioned.map((a) => (
                <Chip key={a.slug} onRemove={() => revoke(a, org.id, org.name)} removeLabel={t.t("Revoke share")}>{a.title}</Chip>
              ))}
            </div>
          )}
        </Card>
      )}

      <PassesCard org={org} hidden={(x) => shownEvents.has(x.event_id!)} onGrant={() => (adventures.length === 0 ? toast.err(t.t("No adventures loaded")) : setOpen({ kind: "grant-pass" }))} onEdit={(pass) => setOpen({ kind: "edit-pass", pass })} />

      <Card title={t.t("Purchases")} flush={purchases.length > 0}>
        <Table<Purchase>
          rows={purchases}
          rowKey={(p) => p.id}
          caption={t.t("Purchases")}
          empty={<p className="adm-muted">{t.t("No purchases.")}</p>}
          columns={[
            { key: "adv", header: t.t("Adventure"), cell: (p) => p.adventure_title || p.adventure_slug },
            { key: "players", header: t.t("Players"), cell: (p) => <span className="adm-mono">{p.max_players}</span>, wide: true },
            { key: "total", header: t.t("Total"), cell: (p) => <span className="adm-mono">{fmtMoney(p.amount_cents, p.currency || "SEK", t.current())}</span> },
            { key: "state", header: t.t("State"), cell: (p) => <PurchaseState p={p} /> },
            { key: "date", header: t.t("Date"), wide: true, cell: (p) => <span className="adm-sub" title={fmtDateTime(p.created_at, t.current())}>{relTime(p.created_at, t.current())}</span> },
            { key: "x", header: <span className="cs-sr">{t.t("Actions")}</span>, align: "end", cell: (p) => <PurchaseActions p={p} reload={reload} /> },
          ]}
        />
      </Card>

      {open?.kind === "rename" && <RenameOrgDialog org={org} onClose={close} onDone={done} />}
      {open?.kind === "add-member" && <AddMemberDialog org={org} onClose={close} onDone={done} />}
      {open?.kind === "grant-pass" && <GrantPassDialog org={org} adventures={adventures} onClose={close} onDone={done} />}
      {open?.kind === "edit-pass" && <EditPassDialog org={org} pass={open.pass} adventures={adventures} onClose={close} onDone={done} />}
      {open?.kind === "grant-slots" && <GrantSlotsDialog org={org} event={open.event} onClose={close} onDone={done} />}
      {open?.kind === "commission" && (
        <PickDialog
          title={t.t("Commission adventure")}
          items={open.candidates}
          label={(a) => a.title}
          onClose={close}
          onPick={(a) => { close(); void act(() => grantShare(a.slug, org.id), t.t("Shared")); }}
        />
      )}
      {open?.kind === "share" && (
        <PickDialog
          title={t.t("Share with organization")}
          items={open.orgs}
          label={(o) => o.name}
          onClose={close}
          onPick={(o) => { const a = open.adventure; close(); void act(() => grantShare(a.slug, o.id), t.t("Shared")); }}
        />
      )}
    </Page>
  );
}

type T = ReturnType<typeof useT>;

interface EventActions {
  add: (e: OrgEvent) => void;
  cancel: (e: OrgEvent) => void;
  pass: (e: OrgEvent) => Assignment | undefined;
  editPass: (a: Assignment) => void;
}

/** When the event started, ended or starts, relative to now. */
function eventWhen(t: T, e: OrgEvent): ReactNode {
  const loc = t.current();
  const at = (ts: string, text: string) => <span title={fmtDateTime(ts, loc)}>{text}</span>;
  if (e.completed_at) return at(e.completed_at, t.t("Ended {{when}}", { when: relTime(e.completed_at, loc) }));
  if (e.cancelled_at) return at(e.cancelled_at, t.t("Cancelled {{when}}", { when: relTime(e.cancelled_at, loc) }));
  if (e.started_at) return at(e.started_at, t.t("Started {{when}}", { when: relTime(e.started_at, loc) }));
  if (e.scheduled_at) return at(e.scheduled_at, t.t("Starts {{when}}", { when: relTime(e.scheduled_at, loc) }));
  return null;
}

function eventColumns(t: T, on: EventActions): Column<OrgEvent>[] {
  return [
    {
      key: "name", header: t.t("Event"), cell: (e) => {
        const when = eventWhen(t, e);
        return (
          <>
            <strong>{e.name || `Event #${e.id}`}</strong>
            {e.public_for_sale && <> <Pill>{t.t("Self-guided")}</Pill></>}
            <div className="adm-sub">{e.adventure_title || e.adventure_slug}{when && <> · {when}</>}</div>
          </>
        );
      },
    },
    { key: "state", header: t.t("State"), cell: (e) => <Pill tone={STATE_TONE[e.state] ?? "neutral"}>{t.t(e.state)}</Pill> },
    { key: "players", header: t.t("Players"), wide: true, cell: (e) => <span className="adm-mono">{e.max_players ? `${e.player_slots_count || 0} / ${e.max_players}` : e.player_slots_count || 0}</span> },
    {
      key: "x", header: <span className="cs-sr">{t.t("Actions")}</span>, align: "end", cell: (e) => {
        const pass = on.pass(e);
        const name = e.name || `Event #${e.id}`;
        return (
          <span className="adm-actions adm-actions--icons">
            {/* Self-guided adventures grow per ticket, not from a pass. */}
            {live(e) && e.assignment_id > 0 && !e.public_for_sale && (
              <Button size="icon" variant="ghost" icon={<Coins size={ICON.sm} aria-hidden />} aria-label={`${t.t("Add players")} · ${name}`} title={t.t("Add players to this event's pass on the org's behalf")} onClick={() => on.add(e)} />
            )}
            {pass && (
              <Button size="icon" variant="ghost" icon={<Pencil size={ICON.sm} aria-hidden />} aria-label={`${t.t("Edit event pass")} · ${name}`} title={t.t("Click to edit player cap")} onClick={() => on.editPass(pass)} />
            )}
            {live(e) && (
              <Button size="icon" variant="ghost" className="danger" icon={<Ban size={ICON.sm} aria-hidden />} aria-label={`${t.t("Cancel event")} · ${name}`} title={t.t("Cancel event")} onClick={() => on.cancel(e)} />
            )}
          </span>
        );
      },
    },
  ];
}

function SetupCard({ org, connect, onTrusted, onParty, onVerified, onPublisher, reload }: { org: Organization; connect: ConnectStatus | null; onTrusted: () => unknown; onParty: () => unknown; onVerified: () => unknown; onPublisher: () => unknown; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [bust, setBust] = useState(0);
  const [busy, setBusy] = useState(false);

  const upload = async (f: File) => {
    setBusy(true);
    try {
      await uploadLogo(org.id, f);
      setBust(Date.now());
      toast.ok(t.t("Logo uploaded"));
      reload();
    } catch (e) {
      toast.err(t.t("Logo upload failed: {{err}}", { err: e instanceof Error ? e.message : String(e) }));
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    if (!(await dialogs.confirm({ title: t.t("Remove the brand logo?"), confirmLabel: t.t("Remove logo"), danger: true }))) return;
    try {
      await deleteLogo(org.id);
      toast.ok(t.t("Logo removed"));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <Card title={t.t("Settings")}>
      <div className="adm-setting">
        <div className="adm-setting__text">
          <strong>{t.t("Logo")}</strong>
          <ImageDrop
            shape="logo"
            src={org.has_logo ? `/org-logo/${org.id}${bust ? `?t=${bust}` : ""}` : undefined}
            label={t.t("Upload logo")}
            title={t.t("Click to upload a logo")}
            accept="image/png,image/jpeg,image/webp,image/svg+xml"
            busy={busy}
            onFile={(f) => void upload(f)}
            onRemove={() => void remove()}
            removeLabel={t.t("Remove logo")}
          />
        </div>
      </div>
      <Toggle checked={org.trusted} onChange={onTrusted} label={t.t("Invoice billing")} hint={t.t("Lets the org pay for event passes by invoice instead of via Stripe.")} />
      <Toggle checked={org.verified_creator} onChange={onVerified} label={t.t("Verified creator")} hint={t.t("Their store listings and party offers go public without review, and the store shows a Verified creator badge. Turning it on approves what of theirs is waiting.")} />
      <Toggle checked={org.party_adventures} onChange={onParty} label={t.t("Party adventures")} hint={t.t("Opens the public creator, where anyone makes a party adventure without an account: /app/party/trail/start/{{slug}}.", { slug: org.slug ?? "" })} />
      <Toggle checked={org.is_publisher} onChange={onPublisher} label={t.t("Catalog publisher")} hint={t.t("The catalog publisher authors the adventures you list in the catalog and commission to other organizations.")} />
      {connect?.platform_fee_enabled && <ConnectRow org={org} status={connect} reload={reload} />}
    </Card>
  );
}

function Toggle({ checked, onChange, label, hint }: { checked: boolean; onChange: () => unknown; label: string; hint: string }) {
  const [busy, setBusy] = useState(false);
  return (
    <label className="adm-toggle">
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={busy}
        onChange={async () => { setBusy(true); try { await onChange(); } finally { setBusy(false); } }}
      />
      <span className="adm-setting__text">
        <strong>{label}</strong>
        <span className="adm-sub">{hint}</span>
      </span>
    </label>
  );
}

function ConnectRow({ org, status, reload }: { org: Organization; status: ConnectStatus; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const refresh = async () => {
    setBusy(true);
    try {
      await refreshConnect(org.id);
      reload();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  const state = !status.account_id
    ? <Pill>{t.t("Not connected")}</Pill>
    : status.charges_enabled
      ? <Pill tone="good">{t.t("Connected — accepting charges")}</Pill>
      : <Pill tone="warn">{t.t("Setup not complete")}</Pill>;
  return (
    <div className="adm-setting">
      <div className="adm-setting__text">
        <strong>{t.t("Stripe Connect")}</strong>
        <span className="adm-inline">
          {state}
          {status.account_id && <code className="adm-mono">{status.account_id}</code>}
        </span>
        {status.charges_enabled && (
          <span className="adm-sub">
            {status.payouts_enabled
              ? t.t("Payouts enabled — money flows to your bank on Stripe's standard schedule.")
              : t.t("Charges accepted but payouts are paused — usually means a missing bank account. Visit your Stripe dashboard to fix.")}
          </span>
        )}
        {status.synced_at && <span className="adm-sub">{t.t("Last verified {{when}}", { when: fmtDateTime(status.synced_at, t.current()) })}</span>}
      </div>
      {status.account_id && <Button size="sm" icon={<RefreshCw size={ICON.sm} aria-hidden />} loading={busy} onClick={refresh}>{t.t("Refresh status")}</Button>}
    </div>
  );
}

/**
 * Event passes not yet used, plus any used one whose event isn't in the
 * events list above (`hidden` says which are); the rest are edited from
 * their event's row.
 */
function PassesCard({ org, hidden, onGrant, onEdit }: { org: Organization; hidden: (a: Assignment) => boolean; onGrant: () => void; onEdit: (a: Assignment) => void }) {
  const t = useT();
  const unused = org.assignments.filter((x) => !x.event_id);
  const used = org.assignments.filter((x) => x.event_id && !hidden(x));
  const inUse = org.assignments.filter((x) => x.event_id && hidden(x)).length;
  const inUseText = inUse > 0 ? t.plural(inUse, { one: t.t("{{n}} in use — edit it from its event above"), other: t.t("{{n}} in use — edit them from their events above") }) : undefined;
  const empty = unused.length === 0 && used.length === 0;
  return (
    <Card
      title={t.t("Event passes")}
      subtitle={empty ? undefined : inUseText}
      actions={<Button size="sm" icon={<Plus size={ICON.sm} aria-hidden />} onClick={onGrant}>{t.t("Grant event pass")}</Button>}
    >
      {empty ? (
        <p className="adm-muted">{inUseText ?? t.t("None — assign an adventure to let them schedule an event.")}</p>
      ) : (
        <div className="adm-chips">
          {unused.map((x) => (
            <button key={x.id} type="button" className="adm-chip adm-chip--button" title={t.t("Click to edit")} onClick={() => onEdit(x)}>
              {t.t(x.max_players === 1 ? "{{slug}} · {{n}} player" : "{{slug}} · {{n}} players", { slug: x.adventure_title || x.adventure_slug, n: x.max_players })}
            </button>
          ))}
          {used.map((x) => (
            <button key={x.id} type="button" className="adm-chip adm-chip--button adm-chip--used" title={t.t("Click to edit player cap")} onClick={() => onEdit(x)}>
              {t.t("{{slug}} · event #{{event}} · max {{max}}", { slug: x.adventure_title || x.adventure_slug, event: x.event_id!, max: x.max_players })}
            </button>
          ))}
        </div>
      )}
    </Card>
  );
}

function Chip({ children, onRemove, removeLabel }: { children: ReactNode; onRemove: () => void; removeLabel: string }) {
  return (
    <span className="adm-chip">
      {children}
      <button type="button" className="adm-chip__x" onClick={onRemove} aria-label={removeLabel} title={removeLabel}>
        <Trash2 size={ICON.xs} aria-hidden />
      </button>
    </span>
  );
}

/**
 * An invoice purchase stays "paid" in the database from the moment the
 * pass is granted, though the org has paid nothing yet: it reads as "to
 * invoice", then "billed" once the operator has sent the invoice.
 */
function PurchaseState({ p }: { p: Purchase }) {
  const t = useT();
  if (p.provider === "invoice" && p.state === "paid") {
    if (p.voided_at) return <Pill tone="bad">{t.t("void")}</Pill>;
    if (p.billed_at) return <Pill tone="good">{t.t("billed")}</Pill>;
    return <Pill tone="warn">{t.t("to invoice")}</Pill>;
  }
  const tone: Tone = p.state === "paid" ? "good" : p.state === "pending" ? "warn" : p.state === "refunded" ? "bad" : "neutral";
  return <Pill tone={tone}>{t.t(p.state)}</Pill>;
}

function PurchaseActions({ p, reload }: { p: Purchase; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);
  const money = fmtMoney(p.amount_cents, p.currency || "SEK", t.current());

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.ok(ok);
      reload();
    } catch (e) {
      toast.err(e);
    } finally {
      if (alive.current) setBusy(false);
    }
  };

  const voidIt = async () => {
    const go = await dialogs.confirm({
      title: t.t("Void"),
      message: t.t("Void this purchase so it won't appear on the next invoice? The players stay on the pass; only the billable line is removed."),
      confirmLabel: t.t("Void"),
      danger: true,
    });
    if (go) await run(() => voidPurchase(p.id), t.t("Voided"));
  };

  // The refund call returns at once; the state flips when Stripe's
  // charge.refunded webhook lands, so poll the row for up to 30 s.
  const refund = async () => {
    const go = await dialogs.password({
      title: t.t("Refund"),
      message: t.t("Refund {{amount}} to the customer? The state flips to \"refunded\" once Stripe confirms.", { amount: money }),
      confirmLabel: t.t("Refund"),
      danger: true,
    });
    if (!go) return;
    setBusy(true);
    try {
      await refundPurchase(p.id, go);
    } catch (e) {
      toast.err(e);
      setBusy(false);
      return;
    }
    toast.ok(t.t("Refund initiated. Confirming with Stripe…"));
    const deadline = Date.now() + 30_000;
    const poll = async () => {
      if (Date.now() > deadline) {
        toast.err(t.t("Refund still processing — check Stripe and refresh in a minute."));
        reload();
        return;
      }
      const r = await getPurchase(p.id).catch(() => null);
      if (r?.state === "refunded") {
        toast.ok(t.t("Refund confirmed."));
        reload();
        return;
      }
      setTimeout(poll, 2000);
    };
    setTimeout(poll, 2000);
  };

  const invoiceOpen = p.provider === "invoice" && p.state === "paid" && !p.voided_at;
  return (
    <span className="adm-actions">
      {p.invoice_url && <a className="btn sm ghost" href={p.invoice_url} target="_blank" rel="noopener">{t.t("Receipt")}</a>}
      {invoiceOpen && (
        <Button size="sm" disabled={busy} onClick={() => run(() => setBilled(p.id, !p.billed_at), p.billed_at ? t.t("Marked unbilled") : t.t("Marked billed"))}>
          {p.billed_at ? t.t("Unmark billed") : t.t("Mark billed")}
        </Button>
      )}
      {invoiceOpen && !p.billed_at && <Button size="sm" variant="danger" disabled={busy} onClick={voidIt}>{t.t("Void")}</Button>}
      {p.state === "paid" && p.provider === "stripe" && (p.pre_migration
        ? <span title={t.t("This purchase was imported from a previous zv deployment. Refund it through the pre-migration Stripe dashboard, not this button.")}><Pill>{t.t("Refund via legacy Stripe dashboard")}</Pill></span>
        : <Button size="sm" variant="danger" disabled={busy} onClick={refund}>{t.t("Refund")}</Button>)}
    </span>
  );
}
