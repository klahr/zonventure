import { canAuthor, readsAsAdmin } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Page, PageHeader, Tabs } from "../../ui/index.ts";
import { AccountTab } from "./Account.tsx";
import { OrganizationTab } from "./Organization.tsx";
import { MembersTab } from "./Members.tsx";
import { AdminsTab } from "./Admins.tsx";
import { AuditTab } from "./Audit.tsx";
import { PurchasesTab } from "./Purchases.tsx";
import { TicketSalesCard } from "./TicketSales.tsx";
import { BillingTab } from "./Billing.tsx";
import { SystemTab } from "./System.tsx";
import { MigrationTab } from "./Migration.tsx";
import { SoftwareUpdateTab } from "./SoftwareUpdate.tsx";
import { IntegrationsTab } from "./Integrations.tsx";
import { ThemesTab } from "./Themes.tsx";
import { PricingTab } from "./Pricing.tsx";
import { LegalTab } from "./Legal.tsx";
import "./settings.css";

/**
 * Settings: one tab per concern, each its own file, shown by role as in
 * the classic console. A tab the user may not see falls back to the
 * first one, so an old or shared link never lands on an empty page.
 */
export function SettingsPage({ tab }: { tab?: string | undefined }) {
  const t = useT();
  const { state } = useSession();
  if (state.status !== "authenticated") return null;
  const u = state.user;
  const admin = u.role === "admin";
  const orgAdmin = u.role === "org_admin";
  const setupOnly = !!u.password_is_default || !!u.totp_setup_required;
  const tabs = [
    { id: "organization", label: t.t("Organization"), visible: orgAdmin },
    { id: "account", label: t.t("Account"), visible: true },
    { id: "members", label: t.t("Members"), visible: orgAdmin },
    { id: "themes", label: t.t("Themes"), visible: canAuthor(u) },
    { id: "admins", label: t.t("Admins"), visible: admin },
    { id: "audit", label: t.t("Audit"), visible: orgAdmin || readsAsAdmin(u) },
    { id: "purchases", label: t.t("Purchases"), visible: orgAdmin || admin },
    { id: "billing", label: t.t("Billing"), visible: orgAdmin },
    { id: "pricing", label: t.t("Pricing"), visible: admin },
    { id: "legal", label: t.t("Legal"), visible: admin },
    { id: "system", label: t.t("System"), visible: admin },
    { id: "migration", label: t.t("Migration"), visible: admin },
    { id: "software-update", label: t.t("Software update"), visible: admin },
    { id: "integrations", label: t.t("Integrations"), visible: orgAdmin },
  ];
  const visible = tabs.filter((x) => x.visible && (!setupOnly || x.id === "account"));
  const current = visible.find((x) => x.id === tab)?.id ?? visible[0]!.id;
  // A known tab this role may not open says so, as other pages do.
  const refused = tabs.some((x) => x.id === tab && !x.visible);
  return (
    <Page>
      <PageHeader title={t.t("Settings")} />
      {refused && (
        <div className="alert alert--warn" role="status">
          {t.t("That settings page isn't open to your account.")}
        </div>
      )}
      <div className="st-tabs">
        <Tabs label={t.t("Settings")} current={current} tabs={visible.map((x) => ({ ...x, href: href({ name: "settings", tab: x.id }) }))} />
      </div>
      <SettingsTab id={current} admin={admin} />
    </Page>
  );
}

function SettingsTab({ id, admin }: { id: string; admin: boolean }) {
  switch (id) {
    case "organization":
      return <OrganizationTab />;
    case "members":
      return <MembersTab />;
    case "themes":
      return <ThemesTab />;
    case "admins":
      return <AdminsTab />;
    case "audit":
      return <AuditTab />;
    case "purchases":
      return (
        <>
          <PurchasesTab />
          {/* An organisation's ticket sales are under Orders; here, the platform's. */}
          {admin && <TicketSalesCard />}
        </>
      );
    case "billing":
      return <BillingTab />;
    case "pricing":
      return <PricingTab />;
    case "legal":
      return <LegalTab />;
    case "system":
      return <SystemTab />;
    case "migration":
      return <MigrationTab />;
    case "software-update":
      return <SoftwareUpdateTab />;
    case "integrations":
      return <IntegrationsTab />;
    default:
      return <AccountTab />;
  }
}
