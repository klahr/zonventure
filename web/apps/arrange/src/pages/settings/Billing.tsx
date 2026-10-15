import { useEffect, useRef, useState, type ReactNode } from "react";
import { Compass, ExternalLink, RefreshCw } from "lucide-react";
import { get, post } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, ErrorBox, HelpPopover, ICON, Loading, Pill, Row, fmtDateTime, relTime, useLoad, useToast } from "../../ui/index.ts";

/** connectStatusDTO, internal/admin/stripe_connect.go. */
interface ConnectStatus {
  account_id?: string;
  charges_enabled: boolean;
  payouts_enabled: boolean;
  synced_at?: string;
  /** Orgs sell into Stripe accounts of their own on this server. */
  platform_fee_enabled: boolean;
}

// Stripe's onboarding sends the browser back with ?stripe_connect=return
// (or =refresh when the link expired). Read it once, then drop it from
// the URL so a reload doesn't sync again.
function takeReturnFlag(): string | null {
  const flag = new URLSearchParams(location.search).get("stripe_connect");
  if (flag) history.replaceState(null, "", location.pathname + location.hash);
  return flag;
}

/**
 * The org's own Stripe account, for its store sales: connect one, resume
 * an unfinished setup, and see whether it takes payments. Buyers pay the
 * org; the platform's price for each team comes out of the sale, and
 * Stripe's card fee is the org's.
 */
export function BillingTab() {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const orgID = state.status === "authenticated" ? state.user.org_id ?? 0 : 0;
  const [flag] = useState(takeReturnFlag);
  const sync = useRef(flag === "return");
  const [starting, setStarting] = useState(false);
  const [opening, setOpening] = useState(false);
  const load = useLoad(
    (signal) => {
      // Fresh from Stripe: have the server re-read the account before
      // showing it, so charges_enabled matches what was just finished.
      if (sync.current) {
        sync.current = false;
        return post<ConnectStatus>(`/api/admin/organizations/${orgID}/stripe-connect/refresh`, undefined, { signal });
      }
      return get<ConnectStatus>(`/api/admin/organizations/${orgID}/stripe-connect/status`, { signal });
    },
    [orgID],
  );

  useEffect(() => {
    if (flag === "return") toast.ok(t.t("Welcome back from Stripe — checking status…"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const start = async () => {
    setStarting(true);
    try {
      const r = await post<{ url?: string }>(`/api/admin/organizations/${orgID}/stripe-connect/start`);
      if (!r?.url) {
        toast.err(t.t("Stripe didn't return an onboarding URL"));
        setStarting(false);
        return;
      }
      location.href = r.url;
    } catch (e) {
      toast.err(e);
      setStarting(false);
    }
  };

  // The tab is opened before the request so popup blockers allow it.
  const openDashboard = async () => {
    setOpening(true);
    const tab = window.open("", "_blank");
    try {
      const r = await post<{ url?: string }>(`/api/admin/organizations/${orgID}/stripe-connect/dashboard`);
      if (!r?.url) throw new Error(t.t("Stripe didn't return a dashboard link"));
      if (tab) tab.location.href = r.url;
      else location.href = r.url;
    } catch (e) {
      tab?.close();
      toast.err(e);
    } finally {
      setOpening(false);
    }
  };

  const about = (
    <HelpPopover
      label={t.t("About your Stripe account")}
      body={t.t("Buyers in your store pay your own Stripe account: you are the seller, and you see payments, refunds and payouts in your Stripe dashboard. The platform's price for each team comes out of the sale; Stripe's card fee is yours, as in any shop.")}
    />
  );
  const card = (subtitle: ReactNode, body: ReactNode, badge?: ReactNode) => (
    <Card title={<>{t.t("Selling in your store")} {badge}</>} subtitle={subtitle} actions={about}>{body}</Card>
  );
  if (!orgID) return <ErrorBox message={t.t("Org admin only.")} />;
  if (load.error && !load.data) return card(null, <ErrorBox message={load.error || t.t("Failed to load Connect status")} retry={load.reload} retryLabel={t.t("Retry")} />);
  if (!load.data) return card(null, <Loading label={t.t("Loading…")} />);
  const s = load.data;
  const loc = t.current();
  const dashboardBtn = <Button icon={<ExternalLink size={ICON.md} aria-hidden />} loading={opening} onClick={() => void openDashboard()}>{t.t("Open Stripe dashboard")}</Button>;
  const refreshBtn = <Button icon={<RefreshCw size={ICON.md} aria-hidden />} loading={load.loading} onClick={load.reload}>{t.t("Refresh status")}</Button>;
  const muted = { margin: 0, color: "var(--dz-muted)", fontSize: "var(--dz-text-sm)" };
  const account = (id: string) => (
    <>
      <code>{id}</code>
      {s.synced_at && <> · <span title={fmtDateTime(s.synced_at, loc)}>{t.t("Last verified {{when}}", { when: relTime(s.synced_at, loc) })}</span></>}
    </>
  );

  if (!s.platform_fee_enabled) {
    // Nothing to connect here, so neither the Connect title nor its popover applies.
    return (
      <Card title={t.t("Selling in your store")}>
        <p style={muted}>{t.t("Ticket sales in your store are paid to this server's own Stripe account, so you have no Stripe account to connect here.")}</p>
      </Card>
    );
  }
  if (!s.account_id) {
    return card(
      t.t("Connect a Stripe account of your own to sell tickets and parties in your store. Setting it up at Stripe takes a few minutes; afterwards you follow payments and payouts in your Stripe dashboard."),
      <Row>
        <Button variant="primary" icon={<Compass size={ICON.md} aria-hidden />} loading={starting} onClick={() => void start()}>{t.t("Connect Stripe")}</Button>
      </Row>,
    );
  }
  if (!s.charges_enabled) {
    return card(
      account(s.account_id),
      <>
        <p style={muted}>{t.t("Stripe still needs more information before your account can accept charges. Resume onboarding to finish.")}</p>
        <Row>
          <Button variant="primary" loading={starting} onClick={() => void start()}>{t.t("Resume onboarding")}</Button>
          {refreshBtn}
        </Row>
      </>,
      <Pill tone="warn">{t.t("Setup incomplete")}</Pill>,
    );
  }
  return card(
    account(s.account_id),
    <>
      {s.payouts_enabled ? (
        <p style={muted}>{t.t("Payouts enabled — Stripe pays your sales out to your bank on the schedule set in your Stripe dashboard.")}</p>
      ) : (
        <Callout variant="warning">{t.t("Charges accepted but payouts are paused — usually means a missing bank account. Visit your Stripe dashboard to fix.")}</Callout>
      )}
      <Row>{dashboardBtn}{refreshBtn}</Row>
    </>,
    <Pill tone="good">{t.t("Connected — accepting charges")}</Pill>,
  );
}
