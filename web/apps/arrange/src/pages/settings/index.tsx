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
  const orgUser = !admin && u.memberships.length > 0;
  const tabs = [
    { id: "organization", label: t.t("Organization"), visible: orgAdmin },
    { id: "account", label: t.t("Account"), visible: true },
    { id: "members", label: t.t("Members"), visible: orgAdmin },
    { id: "themes", label: t.t("Themes"), visible: orgAdmin },
    { id: "admins", label: t.t("Admins"), visible: admin },
    { id: "audit", label: t.t("Audit"), visible: orgAdmin || admin },
    { id: "purchases", label: t.t("Purchases"), visible: orgUser },
    { id: "billing", label: t.t("Billing"), visible: orgAdmin },
    { id: "pricing", label: t.t("Pricing"), visible: admin },
    { id: "legal", label: t.t("Legal"), visible: admin },
    { id: "system", label: t.t("System"), visible: admin },
    { id: "migration", label: t.t("Migration"), visible: admin },
    { id: "software-update", label: t.t("Software update"), visible: admin },
    { id: "integrations", label: t.t("Integrations"), visible: orgUser },
  ].filter((x) => x.visible);
  const current = tabs.find((x) => x.id === tab)?.id ?? tabs[0]!.id;
  return (
    <Page>
      <PageHeader title={t.t("Settings")} />
      <div className="st-tabs">
        <Tabs label={t.t("Settings")} current={current} tabs={tabs.map((x) => ({ ...x, href: href({ name: "settings", tab: x.id }) }))} />
      </div>
      <SettingsTab id={current} />
    </Page>
  );
}

function SettingsTab({ id }: { id: string }) {
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
          <TicketSalesCard />
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
