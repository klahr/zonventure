import { useState } from "react";
import { Copy, ExternalLink, Mail, MailCheck, Pencil, Plus, Printer } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href, navigate } from "../../router.ts";
import {
  Button, Empty, ErrorBox, HelpPopover, ICON, Loading, Page, PageHeader, Pill, Tabs, fmtDateTime, relTime, useDialogs, useLoad, useToast, type Tone,
} from "../../ui/index.ts";
import { SPOTS } from "@zonventure/ui";
import { play } from "@zonventure/api";
import { decide, listPartners, listRedemptions, reject, updatePurchase, updateRedemption, type Redemption } from "./api.ts";
import { AddRedemptionDialog, PostDialog, RejectDialog } from "./dialogs.tsx";
import { PrintQueueButton, printable } from "./PrintQueue.tsx";
import { MoreMenu, type MenuItem } from "../event/Menu.tsx";
import { PartnersCard } from "./Partners.tsx";
import { TicketSalesCard } from "../settings/TicketSales.tsx";
import { langName, localText, regionName, useRedemptionError } from "./shared.ts";
import "./redemptions.css";

const STATES = ["pending", "to_post", "approved", "rejected", "cancelled", "all"] as const;
const LABEL: Record<(typeof STATES)[number], string> = { pending: "Pending", to_post: "To post", approved: "Approved", rejected: "Rejected", cancelled: "Cancelled", all: "All" };
const TONE: Record<string, Tone> = { pending: "warn", approved: "good", rejected: "bad", cancelled: "neutral", paid: "good" };
// Beside the queue: what buyers paid (the org admin's), and the setup.
const SECTIONS = ["sales", "partners"] as const;
type Section = (typeof SECTIONS)[number];
const stateLabel = (s: string) => (s === "paid" ? "Paid" : LABEL[s as (typeof STATES)[number]] ?? s);



/**
 * Gift box codes redeemed on the organisation's public page. Someone
 * checks each code in the partner's own portal, then approves it (the
 * server makes a team and emails its link) or rejects it. Tabs beside
 * the queue hold the ticket sales and the setup: which partners' codes
 * are taken and what their products unlock.
 */
export function RedemptionsPage({ tab }: { tab?: string | undefined }) {
  const t = useT();
  const { state } = useSession();
  const isOrgAdmin = state.status === "authenticated" && state.user.role === "org_admin";
  const section = (SECTIONS as readonly string[]).includes(tab ?? "") && (tab !== "sales" || isOrgAdmin) ? (tab as Section) : null;
  const st = (STATES as readonly string[]).includes(tab ?? "") ? (tab as (typeof STATES)[number]) : "pending";
  // A section still loads the queue, for the tabs' counts.
  const list = useLoad((s) => listRedemptions(st, s), [st]);
  const partners = useLoad((s) => listPartners(s), []);
  const [adding, setAdding] = useState(false);
  const counts = list.data?.counts ?? {};
  const canAdd = (partners.data ?? []).some((p) => p.enabled && (p.products ?? []).some((x) => x.enabled));
  // Whether there is anything at all: to_post overlaps approved, but also
  // holds store purchases, which no other state counts.
  const total = STATES.reduce((a, s) => a + (s === "all" ? 0 : counts[s] ?? 0), 0);
  // Letters print a language at a time: offer the languages the list holds.
  const [lang, setLang] = useState("");
  const all = list.data?.redemptions ?? [];
  const langs = [...new Set(all.filter((r) => (r.letter_languages?.length ?? 0) > 1).map((r) => r.letter_lang).filter(Boolean))].sort();
  const rows = lang && langs.includes(lang) ? all.filter((r) => r.letter_lang === lang) : all;

  return (
    <Page>
      <PageHeader
        title={
          <>
            {t.t("Orders")}
            <HelpPopover
              label={t.t("About orders")}
              body={t.t("Gift box codes redeemed on your public redeem page, and store purchases whose adventure posts a letter. Check each box code in the partner's portal, then approve it to create a team and email its link, or reject it. Print each letter, post it and mark it posted.")}
            />
          </>
        }
        subtitle={list.data && (counts.pending ?? 0) > 0
          ? t.plural(counts.pending ?? 0, { one: t.t("{{n}} code waiting for review"), other: t.t("{{n}} codes waiting for review") })
          : t.t("Gift boxes redeemed on your redeem page, and store purchases with a letter to post.")}
        actions={canAdd && <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setAdding(true)}>{t.t("Add redemption")}</Button>}
      />
      <Tabs
        label={t.t("Orders")}
        current={section ?? st}
        tabs={[
          ...STATES.map((s) => {
            const n = s === "all" ? 0 : counts[s] ?? 0;
            return { id: s, href: href({ name: "redemptions", tab: s }), label: <>{t.t(LABEL[s])}{n > 0 && <span className="cs-count">{n}</span>}</> };
          }),
          ...(isOrgAdmin ? [{ id: "sales", href: href({ name: "redemptions", tab: "sales" }), label: t.t("Ticket sales") }] : []),
          { id: "partners", href: href({ name: "redemptions", tab: "partners" }), label: t.t("Partners") },
        ]}
      />
      {section === "sales" ? (
        <TicketSalesCard />
      ) : section === "partners" ? (
        partners.error && !partners.data ? (
          <ErrorBox message={partners.error} retry={partners.reload} retryLabel={t.t("Retry")} />
        ) : partners.data ? (
          <PartnersCard partners={partners.data} isOrgAdmin={isOrgAdmin} reload={partners.reload} />
        ) : (
          <Loading label={t.t("Loading…")} />
        )
      ) : (
        <>
          {(langs.length > 1 || rows.some(printable)) && (
            <div className="rd-toolbar">
              {langs.length > 1 && (
                <label className="rd-filter">
                  <span>{t.t("Envelope language")}</span>
                  <select value={langs.includes(lang) ? lang : ""} onChange={(e) => setLang(e.target.value)}>
                    <option value="">{t.t("All languages")}</option>
                    {langs.map((l) => <option key={l} value={l}>{langName(l, t.current())}</option>)}
                  </select>
                </label>
              )}
              <PrintQueueButton orders={rows} reload={list.reload} />
            </div>
          )}
          {list.error && !list.data ? (
            <ErrorBox message={list.error} retry={list.reload} retryLabel={t.t("Retry")} />
          ) : !list.data ? (
            <Loading label={t.t("Loading…")} />
          ) : rows.length === 0 ? (
            <Empty title={total === 0 ? t.t("No redemptions yet.") : st === "pending" ? t.t("No redemptions waiting for review.") : st === "to_post" ? t.t("Nothing left to post.") : t.t("No redemptions here.")} {...(total === 0 ? { illustration: SPOTS.noRedemptions } : {})} />
          ) : (
            <ul className="cs-records">
              {rows.map((rd) => <RedemptionItem key={`${rd.kind}-${rd.id}`} rd={rd} showState={st === "all"} reload={list.reload} />)}
            </ul>
          )}
        </>
      )}
      {adding && partners.data && (
        <AddRedemptionDialog
          partners={partners.data}
          onClose={() => setAdding(false)}
          onDone={(state) => {
            setAdding(false);
            // Show the list it landed in: approved, or pending if it couldn't be.
            const to = state === "approved" ? "approved" : "pending";
            if (st === to) list.reload();
            else navigate({ name: "redemptions", tab: to });
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
  const [rejecting, setRejecting] = useState(false);
  const [busy, setBusy] = useState(false);
  const locale = t.current();
  const purchase = rd.kind === "purchase";
  const country = rd.country ? regionName(rd.country, locale) : "";
  const address = [rd.street_address, rd.address_2, [rd.postal_code, rd.city].filter(Boolean).join(" "), country].filter(Boolean);
  const save = (body: { posted?: boolean; post_at?: string; note?: string; letter_lang?: string }) =>
    purchase ? updatePurchase(rd.id, body) : updateRedemption(rd.id, body);

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

  const email = rd.email || t.t("the customer");
  const act = async (action: "approve" | "cancel") => {
    const copy = {
      approve: {
        title: t.t("Approve this redemption?"),
        body: t.t("A team is created on “{{event}}” and its link is emailed to {{email}}. Only approve once you have checked the code in the partner's portal.", { event: rd.event_name || rd.product_name || "", email }),
        btn: t.t("Approve"), danger: false, done: t.t("Approved — the team link is on its way"),
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
    <span className="cs-sub" title={fmtDateTime(rd.decided_at, locale)}>
      {rd.decided_by ? t.t("{{when}} by {{who}}", { when: relTime(rd.decided_at, locale), who: rd.decided_by }) : relTime(rd.decided_at, locale)}
    </span>
  );
  const postItem: MenuItem = { label: t.t("Post date and note"), icon: <Pencil size={ICON.sm} aria-hidden />, onClick: () => setEditing(true) };
  const togglePosted = () => run(() => save({ posted: !rd.posted_at }), t.t("Saved"));
  const postedIcon = rd.posted_at ? <MailCheck size={ICON.sm} aria-hidden /> : <Mail size={ICON.sm} aria-hidden />;
  const postedLabel = rd.posted_at ? t.t("Mark as not posted") : t.t("Mark as posted");
  const postedItem: MenuItem = { label: postedLabel, icon: postedIcon, onClick: () => void togglePosted() };
  const cancelItem: MenuItem = { label: t.t("Cancel redemption"), danger: true, onClick: () => void act("cancel") };
  const prints = !closed && (rd.print ?? []).map((p) => (
    <Button key={p.id} size="sm" icon={<Printer size={ICON.sm} aria-hidden />}
      title={t.t("Open the PDF to print it")}
      onClick={() => window.open(play.mediaURL(p.media_id), "_blank", "noopener")}>
      {(rd.letter_languages?.length ?? 0) > 1 ? `${p.name} (${langName(p.lang, locale)})` : p.name}
    </Button>
  ));
  return (
    <li className="cs-card">
      <div className="cs-record__cols">
        {purchase ? (
        <div className="cs-record__col">
          <span className="cs-sub">{t.t("Order")}</span>
          <strong>{t.t("Store purchase")}</strong>
          <span className="cs-sub" title={fmtDateTime(rd.created_at, locale)}>{t.t("Paid {{when}}", { when: relTime(rd.created_at, locale) })}</span>
        </div>
        ) : (
        <div className="cs-record__col">
          <span className="cs-sub">{t.t("Code")}</span>
          <strong>{rd.partner_name}</strong>
          <span className="rd-codes">
            {rd.box_number && (
              <button type="button" className="rd-code" title={t.t("Copy")} onClick={() => copy(rd.box_number)}>
                {localText(rd.partner_fields?.[0]?.label, locale) || t.t("Box")} {rd.box_number}<Copy size={ICON.xs} aria-hidden />
              </button>
            )}
            <button type="button" className="rd-code" title={t.t("Copy")} onClick={() => copy(rd.security_code)}>
              {rd.security_code}<Copy size={ICON.xs} aria-hidden />
            </button>
          </span>
          {rd.state === "pending" && rd.partner_portal_url && (
            <a className="rd-portal cs-sub" href={rd.partner_portal_url} target="_blank" rel="noopener noreferrer">
              <ExternalLink size={ICON.xs} aria-hidden />{t.t("Check the code in {{partner}}'s portal", { partner: rd.partner_name })}
            </a>
          )}
          <span className="cs-sub" title={fmtDateTime(rd.created_at, locale)}>{t.t("Received {{when}}", { when: relTime(rd.created_at, locale) })}</span>
        </div>
        )}
        <div className="cs-record__col">
          <span className="cs-sub">{t.t("Product")}</span>
          <span>{rd.product_name || "—"}</span>
          {rd.event_id > 0 && <a className="cs-sub" href={href({ name: "event", id: rd.event_id })}>{rd.event_name || `#${rd.event_id}`}</a>}
        </div>
        <div className="cs-record__col">
          <span className="cs-sub">{t.t("Customer")}</span>
          <strong>{rd.name}</strong>
          {rd.email && <a className="cs-sub" href={`mailto:${rd.email}`}>{rd.email}</a>}
          {address.length > 0 && <span className="cs-sub rd-address">{address.join("\n")}</span>}
        </div>
        {(!closed || rd.posted_at || rd.post_at || rd.note) && (
          <div className="cs-record__col">
            <span className="cs-sub">{t.t("Post")}</span>
            {rd.post_at && <span className="cs-sub">{t.t("Post on {{date}}", { date: rd.post_at })}</span>}
            <span className="rd-pills">
              {rd.posted_at ? <span title={fmtDateTime(rd.posted_at, locale)}><Pill tone="good">{t.t("Posted")}</Pill></span> : <Pill>{t.t("Not posted")}</Pill>}
              {rd.letter_lang && (rd.letter_languages?.length ?? 0) > 1 && (
                <Pill title={t.t("Envelope language")}>{langName(rd.letter_lang, locale)}</Pill>
              )}
            </span>
            {rd.note && <span className="cs-sub rd-note">{rd.note}</span>}
          </div>
        )}
        {(showState || decided || ((rd.state === "approved" || purchase) && rd.team_id)) && (
          <div className="cs-record__col">
            <span className="cs-sub">{showState ? t.t("State") : t.t(stateLabel(rd.state))}</span>
            {showState && <span><Pill tone={TONE[rd.state] ?? "neutral"}>{t.t(stateLabel(rd.state))}</Pill></span>}
            {decided}
            {(rd.state === "approved" || purchase) && rd.team_id && (
              <a className="cs-sub" href={href({ name: "event", id: rd.event_id })}>{t.t("Team: {{name}}", { name: rd.team_name || `#${rd.team_id}` })}</a>
            )}
          </div>
        )}
      </div>
      <div className="cs-record__bar">
        {rd.state === "pending" && (
          <>
            <Button size="sm" disabled={busy} onClick={() => act("approve")}>{t.t("Approve")}</Button>
            <Button size="sm" disabled={busy} onClick={() => setRejecting(true)}>{t.t("Reject")}</Button>
            {prints}
            <span className="cs-spacer" />
            <MoreMenu size="sm" label={t.t("More")} items={[postItem, postedItem, { separator: true }, cancelItem]} />
          </>
        )}
        {rd.state === "approved" && (
          <>
            {prints}
            <Button size="sm" disabled={busy} icon={postedIcon} onClick={() => void togglePosted()}>{postedLabel}</Button>
            <span className="cs-spacer" />
            <MoreMenu size="sm" label={t.t("More")} items={[postItem, { separator: true }, cancelItem]} />
          </>
        )}
        {purchase && (
          <>
            {prints}
            <Button size="sm" disabled={busy} icon={postedIcon} onClick={() => void togglePosted()}>{postedLabel}</Button>
            <span className="cs-spacer" />
            <MoreMenu size="sm" label={t.t("More")} items={[postItem]} />
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
          onSave={async (body) => { setEditing(false); await run(() => save(body), t.t("Saved")); }}
        />
      )}
      {rejecting && (
        <RejectDialog
          email={email}
          onClose={() => setRejecting(false)}
          onReject={async (reason) => { setRejecting(false); await run(() => reject(rd.id, reason), t.t("Redemption rejected")); }}
        />
      )}
    </li>
  );
}

