import { useState } from "react";
import { Plus, Search } from "lucide-react";
import { isGlobalAdmin } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Card, Dialog, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, Row, Tile, Tiles, fmtDate, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { createOrganization, listOrganizations, partiesByHost, resendHostLink, type HostParty, type Organization } from "./api.ts";
import { OrgLogo } from "./OrgLogo.tsx";
import "./admin.css";

// Fewer than this fit on a screen; a search box would only be noise.
const SEARCH_FROM = 6;

/** The platform admin's customer list: every organisation, searchable, each a link to its page. */
export function OrganizationsPage() {
  const t = useT();
  const { state } = useSession();
  // Support reads the list but creates no organizations.
  const full = state.status === "authenticated" && isGlobalAdmin(state.user);
  const orgs = useLoad((s) => listOrganizations(s), []);
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);

  const all = orgs.data ?? [];
  const needle = q.trim().toLowerCase();
  const shown = needle ? all.filter((o) => o.name.toLowerCase().includes(needle)) : all;
  const running = all.reduce((n, o) => n + o.events_running, 0);

  return (
    <Page>
      <PageHeader
        title={t.t("Organizations")}
        subtitle={orgs.data && all.length > 0 && (
          <>
            {t.plural(all.length, { one: t.t("{{n}} organization"), other: t.t("{{n}} organizations") })}
            {running > 0 && <> · {t.plural(running, { one: t.t("{{n}} event running"), other: t.t("{{n}} events running") })}</>}
          </>
        )}
        actions={full && <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New organization")}</Button>}
      />
      {orgs.error && !orgs.data ? (
        <ErrorBox message={t.t("Couldn't load organizations ({{err}}).", { err: orgs.error })} retry={orgs.reload} retryLabel={t.t("Retry")} />
      ) : !orgs.data ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <>
          {all.length >= SEARCH_FROM && (
            <label className="adm-search">
              <Search size={ICON.md} aria-hidden />
              <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder={t.t("Search organizations…")} aria-label={t.t("Search organizations")} autoComplete="off" />
            </label>
          )}
          {all.length === 0 ? (
            <Empty title={t.t("No organizations yet.")} />
          ) : shown.length === 0 ? (
            <Empty title={t.t("No organizations match “{{q}}”.", { q: q.trim() })} />
          ) : (
            <Tiles>
              {shown.map((o) => <OrgTile key={o.id} org={o} />)}
            </Tiles>
          )}
        </>
      )}
      <HostLinkCard />
      {creating && <CreateOrgDialog onClose={() => setCreating(false)} />}
    </Page>
  );
}

/** Support: find a host's parties by the email that booked them and mail a lost host link again. */
function HostLinkCard() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const locale = t.current();
  const [email, setEmail] = useState("");
  const [found, setFound] = useState<{ email: string; parties: HostParty[] } | null>(null);
  const [busy, setBusy] = useState<number | "search" | null>(null);
  const search = async () => {
    const e = email.trim();
    if (!e) return;
    setBusy("search");
    try {
      setFound({ email: e, parties: await partiesByHost(e) });
    } catch (err) {
      toast.err(err);
    }
    setBusy(null);
  };
  const resend = async (p: HostParty) => {
    if (!found) return;
    // The old link stops working at once, so say which party and ask first.
    const go = await dialogs.confirm({
      title: t.t("Replace the host link for “{{name}}”?", { name: p.name || p.adventure }),
      message: t.t("The host's current link stops working at once, and a new one is mailed to {{email}}.", { email: found.email }),
      confirmLabel: t.t("Replace and send"),
      danger: true,
    });
    if (!go) return;
    setBusy(p.id);
    try {
      await resendHostLink(p.id, found.email);
      toast.ok(t.t("New host link sent to {{email}}. The old link no longer works.", { email: found.email }));
    } catch (err) {
      toast.err(err);
    }
    setBusy(null);
  };
  return (
    <Card title={t.t("Lost host link")} subtitle={t.t("Find a party by the email that booked it and send the host a new link.")}>
      <form onSubmit={(e) => { e.preventDefault(); void search(); }}>
        <Row>
          <Field label={t.t("Host's email")}>
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="off" />
          </Field>
          <Button type="submit" loading={busy === "search"} disabled={!email.trim()}>{t.t("Find parties")}</Button>
        </Row>
      </form>
      {found && (found.parties.length === 0 ? (
        <p className="cs-note cs-note--md">{t.t("No party still to come or under way was booked with {{email}}.", { email: found.email })}</p>
      ) : (
        <ul style={{ listStyle: "none", padding: 0, margin: 0 }}>
          {found.parties.map((p) => (
            <li key={p.id}>
              <Row>
                <span><strong>{p.name || p.adventure}</strong> · {p.adventure}{p.org_name && <> · {p.org_name}</>} · {fmtDate(p.created_at, locale)}</span>
                <Button size="sm" loading={busy === p.id} onClick={() => void resend(p)}>{t.t("Replace host link")}</Button>
              </Row>
            </li>
          ))}
        </ul>
      ))}
    </Card>
  );
}

function OrgTile({ org }: { org: Organization }) {
  const t = useT();
  const members = org.members.length;
  return (
    <Tile
      href={href({ name: "organization", id: org.id })}
      media={<OrgLogo org={org} size={40} />}
      title={org.name}
      meta={
        <>
          {t.plural(members, { one: t.t("{{n}} user"), other: t.t("{{n}} users") })} · {t.plural(org.players_total, { one: t.t("{{n}} player"), other: t.t("{{n}} players") })} · {t.plural(org.events_total, { one: t.t("{{n}} event"), other: t.t("{{n}} events") })}
        </>
      }
    >
      {(org.events_running > 0 || org.trusted || org.verified_creator || org.party_adventures || org.is_publisher) && (
        <span className="adm-inline">
          {org.events_running > 0 && <Pill tone="good">{t.t("{{n}} running", { n: org.events_running })}</Pill>}
          {org.trusted && <Pill tone="good">{t.t("Invoice billing")}</Pill>}
          {org.verified_creator && <Pill tone="good">{t.t("Verified creator")}</Pill>}
          {org.party_adventures && <Pill tone="good">{t.t("Public party creator")}</Pill>}
          {org.is_publisher && <Pill tone="accent">{t.t("Catalog publisher")}</Pill>}
        </span>
      )}
    </Tile>
  );
}

function CreateOrgDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!name.trim()) return;
    setBusy(true);
    try {
      const o = await createOrganization(name.trim());
      toast.ok(t.t("Organization created"));
      onClose();
      if (o?.id) navigate({ name: "organization", id: o.id });
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("New organization")}
      onClose={onClose}
      onSubmit={submit}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!name.trim()}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Name")}>
        <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus maxLength={200} />
      </Field>
    </Dialog>
  );
}
