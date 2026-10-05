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
 * Stripe Connect for the org's store: connect an account, resume an
 * unfinished onboarding, and see whether charges and payouts flow.
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

  // Login links are single-use and short-lived, so mint one per click.
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
      label={t.t("About Stripe Connect")}
      body={t.t("Stripe Connect pays out your org's ticket sales. Buyers pay the platform, which keeps a per-charge fee and transfers the rest to your Stripe account; Stripe then pays it out to your bank.")}
    />
  );
  const card = (subtitle: ReactNode, body: ReactNode, badge?: ReactNode) => (
    <Card title={<>{t.t("Stripe Connect")} {badge}</>} subtitle={subtitle} actions={about}>{body}</Card>
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
    return card(
      null,
      <p style={muted}>{t.t("Marketplace mode is off on this server. The operator hasn't configured ZV_PLATFORM_FEE_PERCENT_BPS, so /store sales would land directly in the platform account with no per-charge fee. No Connect setup needed.")}</p>,
    );
  }
  if (!s.account_id) {
    return card(
      t.t("Connect a Stripe account to start selling tickets to your self-guided adventures in your store. Stripe-hosted verification takes a few minutes; afterwards you follow your balance and payouts in the Stripe dashboard."),
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
        <p style={muted}>{t.t("Payouts enabled — your balance is paid out to your bank once a month, on the 1st.")}</p>
      ) : (
        <Callout variant="warning">{t.t("Charges accepted but payouts are paused — usually means a missing bank account. Visit your Stripe dashboard to fix.")}</Callout>
      )}
      <Row>{dashboardBtn}{refreshBtn}</Row>
    </>,
    <Pill tone="good">{t.t("Connected — accepting charges")}</Pill>,
  );
}
