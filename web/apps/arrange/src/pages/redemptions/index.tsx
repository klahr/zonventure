import { useState } from "react";
import { Copy, Mail, MailCheck, Pencil, Plus } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href, navigate } from "../../router.ts";
import {
  Button, Empty, ErrorBox, HelpPopover, ICON, Loading, Page, PageHeader, Pill, Tabs, fmtDateTime, relTime, useDialogs, useLoad, useToast, type Tone,
} from "../../ui/index.ts";
import { decide, listPartners, listRedemptions, updateRedemption, type Redemption } from "./api.ts";
import { AddRedemptionDialog, PostDialog } from "./dialogs.tsx";
import { MoreMenu, type MenuItem } from "../event/Menu.tsx";
import { PartnersCard } from "./Partners.tsx";
import { partnerLabel, useRedemptionError } from "./shared.ts";
import "./redemptions.css";

const STATES = ["pending", "approved", "rejected", "cancelled", "all"] as const;
const LABEL: Record<(typeof STATES)[number], string> = { pending: "Pending", approved: "Approved", rejected: "Rejected", cancelled: "Cancelled", all: "All" };
const TONE: Record<string, Tone> = { pending: "warn", approved: "good", rejected: "bad", cancelled: "neutral" };

/**
 * Gift box codes redeemed on the organisation's public page. Someone
 * checks each code in the partner's own portal, then approves it (the
 * server makes a team and emails its link) or rejects it. Below, the
 * setup: which partners' codes are taken and what their products unlock.
 */
export function RedemptionsPage({ tab }: { tab?: string | undefined }) {
  const t = useT();
  const { state } = useSession();
  const st = (STATES as readonly string[]).includes(tab ?? "") ? (tab as (typeof STATES)[number]) : "pending";
  const isOrgAdmin = state.status === "authenticated" && state.user.role === "org_admin";
  const list = useLoad((s) => listRedemptions(st, s), [st]);
  const partners = useLoad((s) => listPartners(s), []);
  const [adding, setAdding] = useState(false);
  const counts = list.data?.counts ?? {};
  const canAdd = (partners.data ?? []).some((p) => p.enabled && (p.products ?? []).some((x) => x.enabled));
  const total = Object.values(counts).reduce((a, b) => a + b, 0);
  const rows = list.data?.redemptions ?? [];

  return (
    <Page>
      <PageHeader
        title={
          <>
            {t.t("Box redemptions")}
            <HelpPopover
              label={t.t("About box redemptions")}
              body={t.t("Gift box codes redeemed on your public redeem page. Check each code in the partner's portal, then approve it to create a team and email its link, or reject it.")}
            />
          </>
        }
        subtitle={list.data && (counts.pending ?? 0) > 0
          ? t.plural(counts.pending ?? 0, { one: t.t("{{n}} code waiting for review"), other: t.t("{{n}} codes waiting for review") })
          : t.t("Gift box codes redeemed on your public redeem page.")}
        actions={canAdd && <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setAdding(true)}>{t.t("Add redemption")}</Button>}
      />
      {(total > 0 || st !== "pending") && <Tabs
        label={t.t("Box redemptions")}
        current={st}
        tabs={STATES.map((s) => {
          const n = s === "all" ? 0 : counts[s] ?? 0;
          return { id: s, href: href({ name: "redemptions", tab: s }), label: <>{t.t(LABEL[s])}{n > 0 && <span className="rd-count">{n}</span>}</> };
        })}
      />}
      {list.error && !list.data ? (
        <ErrorBox message={list.error} retry={list.reload} retryLabel={t.t("Retry")} />
      ) : !list.data ? (
        <Loading label={t.t("Loading…")} />
      ) : rows.length === 0 ? (
        <Empty title={total === 0 ? t.t("No redemptions yet.") : st === "pending" ? t.t("No redemptions waiting for review.") : t.t("No redemptions here.")} />
      ) : (
        <ul className="rd-list">
          {rows.map((rd) => <RedemptionItem key={rd.id} rd={rd} showState={st === "all"} reload={list.reload} />)}
        </ul>
      )}
      {partners.error && !partners.data ? (
        <ErrorBox message={partners.error} retry={partners.reload} retryLabel={t.t("Retry")} />
      ) : partners.data ? (
        <PartnersCard partners={partners.data} isOrgAdmin={isOrgAdmin} reload={partners.reload} />
      ) : null}
      {adding && partners.data && (
        <AddRedemptionDialog
          partners={partners.data}
          onClose={() => setAdding(false)}
          onDone={() => {
            setAdding(false);
            // It lands as pending: show that list.
            if (st === "pending") list.reload();
            else navigate({ name: "redemptions", tab: "pending" });
          }}
        />
      )}
    </Page>
  );
}

function RedemptionItem({ rd, showState, reload }: { rd: Redemption; showState: boolean; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const errText = useRedemptionError();
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const locale = t.current();
  const address = [rd.street_address, rd.address_2, [rd.postal_code, rd.city].filter(Boolean).join(" ")].filter(Boolean);

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.ok(ok);
      reload();
    } catch (e) {
      toast.err(errText(e));
    } finally {
      setBusy(false);
    }
  };

  const act = async (action: "approve" | "reject" | "cancel") => {
    const email = rd.email || t.t("the customer");
    const copy = {
      approve: {
        title: t.t("Approve this redemption?"),
        body: t.t("A team is created on “{{event}}” and its link is emailed to {{email}}. Only approve once you have checked the code in the partner's portal.", { event: rd.event_name || rd.product_name || "", email }),
        btn: t.t("Approve"), danger: false, done: t.t("Approved — the team link is on its way"),
      },
      reject: {
        title: t.t("Reject this redemption?"),
        body: t.t("{{email}} is told by email that the code couldn't be verified.", { email }),
        btn: t.t("Reject"), danger: true, done: t.t("Redemption rejected"),
      },
      cancel: rd.state === "approved"
        ? {
            title: t.t("Cancel this approved redemption?"),
            body: t.t("The team's links stop working straight away and {{email}} is told by email. This can't be undone.", { email }),
            btn: t.t("Cancel redemption"), danger: true, done: t.t("Redemption cancelled"),
          }
        : {
            title: t.t("Cancel this redemption?"),
            body: t.t("{{email}} is told by email that the redemption was cancelled.", { email }),
            btn: t.t("Cancel redemption"), danger: true, done: t.t("Redemption cancelled"),
          },
    }[action];
    const ok = await dialogs.confirm({ title: copy.title, message: copy.body, confirmLabel: copy.btn, cancelLabel: t.t("Back"), danger: copy.danger });
    if (ok) await run(() => decide(rd.id, action), copy.done);
  };

  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      toast.ok(t.t("Copied"));
    } catch {
      toast.err(t.t("Copy failed"));
    }
  };

  const closed = rd.state === "rejected" || rd.state === "cancelled";
  const decided = rd.decided_at && (
    <span className="rd-sub" title={fmtDateTime(rd.decided_at, locale)}>
      {rd.decided_by ? t.t("{{when}} by {{who}}", { when: relTime(rd.decided_at, locale), who: rd.decided_by }) : relTime(rd.decided_at, locale)}
    </span>
  );
  const postItem: MenuItem = { label: t.t("Post date and note"), icon: <Pencil size={ICON.sm} aria-hidden />, onClick: () => setEditing(true) };
  const togglePosted = () => run(() => updateRedemption(rd.id, { posted: !rd.posted_at }), t.t("Saved"));
  const postedIcon = rd.posted_at ? <MailCheck size={ICON.sm} aria-hidden /> : <Mail size={ICON.sm} aria-hidden />;
  const postedLabel = rd.posted_at ? t.t("Mark as not posted") : t.t("Mark as posted");
  const postedItem: MenuItem = { label: postedLabel, icon: postedIcon, onClick: () => void togglePosted() };
  const cancelItem: MenuItem = { label: t.t("Cancel redemption"), danger: true, onClick: () => void act("cancel") };
  return (
    <li className="rd-item">
      <div className="rd-item__main">
        <div className="rd-item__col">
          <span className="rd-label">{t.t("Code")}</span>
          <strong>{partnerLabel(rd.partner_kind, rd.partner_name)}</strong>
          <span className="rd-codes">
            {rd.box_number && (
              <button type="button" className="rd-code" title={t.t("Copy")} onClick={() => copy(rd.box_number)}>
                {t.t("Box")} {rd.box_number}<Copy size={ICON.xs} aria-hidden />
              </button>
            )}
            <button type="button" className="rd-code" title={t.t("Copy")} onClick={() => copy(rd.security_code)}>
              {rd.security_code}<Copy size={ICON.xs} aria-hidden />
            </button>
          </span>
          <span className="rd-sub" title={fmtDateTime(rd.created_at, locale)}>{t.t("Received {{when}}", { when: relTime(rd.created_at, locale) })}</span>
        </div>
        <div className="rd-item__col">
          <span className="rd-label">{t.t("Product")}</span>
          <span>{rd.product_name || "—"}</span>
          {rd.event_id > 0 && <a className="rd-sub" href={href({ name: "event", id: rd.event_id })}>{rd.event_name || `#${rd.event_id}`}</a>}
        </div>
        <div className="rd-item__col">
          <span className="rd-label">{t.t("Customer")}</span>
          <strong>{rd.name}</strong>
          {rd.email && <a className="rd-sub" href={`mailto:${rd.email}`}>{rd.email}</a>}
          {address.length > 0 && <span className="rd-sub rd-address">{address.join("\n")}</span>}
        </div>
        {(!closed || rd.posted_at || rd.post_at || rd.note) && (
          <div className="rd-item__col">
            <span className="rd-label">{t.t("Post")}</span>
            {rd.post_at && <span className="rd-sub">{t.t("Post on {{date}}", { date: rd.post_at })}</span>}
            <span>{rd.posted_at ? <span title={fmtDateTime(rd.posted_at, locale)}><Pill tone="good">{t.t("Posted")}</Pill></span> : <Pill>{t.t("Not posted")}</Pill>}</span>
            {rd.note && <span className="rd-sub rd-note">{rd.note}</span>}
          </div>
        )}
        {(showState || decided || (rd.state === "approved" && rd.team_id)) && (
          <div className="rd-item__col">
            <span className="rd-label">{showState ? t.t("State") : t.t(LABEL[rd.state] ?? rd.state)}</span>
            {showState && <span><Pill tone={TONE[rd.state] ?? "neutral"}>{t.t(LABEL[rd.state] ?? rd.state)}</Pill></span>}
            {decided}
            {rd.state === "approved" && rd.team_id && (
              <a className="rd-sub" href={href({ name: "event", id: rd.event_id })}>{t.t("Team: {{name}}", { name: rd.team_name || `#${rd.team_id}` })}</a>
            )}
          </div>
        )}
      </div>
      <div className="rd-item__actions">
        {rd.state === "pending" && (
          <>
            <Button size="sm" disabled={busy} onClick={() => act("approve")}>{t.t("Approve")}</Button>
            <Button size="sm" disabled={busy} onClick={() => act("reject")}>{t.t("Reject")}</Button>
            <span className="rd-spacer" />
            <MoreMenu size="sm" label={t.t("More")} items={[postItem, postedItem, { separator: true }, cancelItem]} />
          </>
        )}
        {rd.state === "approved" && (
          <>
            <Button size="sm" disabled={busy} icon={postedIcon} onClick={() => void togglePosted()}>{postedLabel}</Button>
            <span className="rd-spacer" />
            <MoreMenu size="sm" label={t.t("More")} items={[postItem, { separator: true }, cancelItem]} />
          </>
        )}
        {closed && (
          <Button size="sm" variant="ghost" disabled={busy} icon={<Pencil size={ICON.sm} aria-hidden />} onClick={() => setEditing(true)}>{t.t("Post date and note")}</Button>
        )}
      </div>
      {editing && (
        <PostDialog
          rd={rd}
          onClose={() => setEditing(false)}
          onSave={async (body) => { setEditing(false); await run(() => updateRedemption(rd.id, body), t.t("Saved")); }}
        />
      )}
    </li>
  );
}

