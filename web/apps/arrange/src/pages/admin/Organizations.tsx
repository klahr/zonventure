import { useState } from "react";
import { Plus, Search } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href, navigate } from "../../router.ts";
import { Button, Dialog, Empty, ErrorBox, Field, ICON, Loading, Page, PageHeader, Pill, Tile, Tiles, useLoad, useToast } from "../../ui/index.ts";
import { createOrganization, listOrganizations, type Organization } from "./api.ts";
import { OrgLogo } from "./OrgLogo.tsx";
import "./admin.css";

// Fewer than this fit on a screen; a search box would only be noise.
const SEARCH_FROM = 6;

/** The platform admin's customer list: every organisation, searchable, each a link to its page. */
export function OrganizationsPage() {
  const t = useT();
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
        actions={<Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setCreating(true)}>{t.t("New organization")}</Button>}
      />
      {orgs.error && !orgs.data ? (
        <ErrorBox message={t.t("Couldn't load organizations ({{err}}).", { err: orgs.error })} retry={orgs.reload} retryLabel={t.t("Retry")} />
      ) : !orgs.data ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <>
          {all.length >= SEARCH_FROM && (
            <label className="ad-search">
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
      {creating && <CreateOrgDialog onClose={() => setCreating(false)} />}
    </Page>
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
      {(org.events_running > 0 || org.trusted || org.party_adventures || org.is_publisher) && (
        <span className="ad-inline">
          {org.events_running > 0 && <Pill tone="good">{t.t("{{n}} running", { n: org.events_running })}</Pill>}
          {org.trusted && <Pill tone="good">{t.t("Invoice billing")}</Pill>}
          {org.party_adventures && <Pill tone="good">{t.t("Party adventures")}</Pill>}
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
