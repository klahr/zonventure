import { useEffect, useState } from "react";
import type { Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, href, type Route } from "../../router.ts";
import { Button, Dialog } from "../../ui/index.ts";
import { activeMembership, pricing } from "../events/api.ts";

export type OnboardingVariant = "arranger" | "publisher" | "admin";

export function onboardingVariant(user: Whoami): OnboardingVariant {
  if (user.role === "admin") return "admin";
  return CATALOG_ENABLED && activeMembership(user)?.is_publisher ? "publisher" : "arranger";
}

/**
 * The one-time welcome after signing up. Dismissal sticks in
 * localStorage per account, and it is marked shown the moment it
 * opens, so it never comes back — Settings → Account → Help can open
 * OnboardingDialog again.
 */
export function Onboarding({ user }: { user: Whoami }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const key = "zv_onboarded_" + user.id;
    try {
      if (localStorage.getItem(key) === "1") return;
    } catch {
      return;
    }
    const timer = setTimeout(() => {
      // A reset or email-change link is a task of its own; don't cover it.
      if (/^#\/?(reset|change-email)/.test(location.hash)) return;
      try {
        localStorage.setItem(key, "1");
      } catch {
        /* shown this once anyway */
      }
      setOpen(true);
    }, 200);
    return () => clearTimeout(timer);
  }, [user.id]);
  if (!open) return null;
  return <OnboardingDialog user={user} onClose={() => setOpen(false)} />;
}

export function OnboardingDialog({ user, onClose }: { user: Whoami; onClose: () => void }) {
  const t = useT();
  const variant = onboardingVariant(user);
  const steps: { title: string; text: string }[] =
    variant === "admin"
      ? [
          { title: t.t("Create an organization"), text: t.t("one tenant per customer. They run their own events.") },
          { title: t.t("Add a user to it"), text: t.t("an org_admin can invite team members and run events.") },
          { title: t.t("Choose a catalog publisher"), text: t.t("the organization that authors the catalog; you list and commission its adventures.") },
        ]
      : variant === "publisher"
        ? [
            { title: t.t("Author an adventure"), text: t.t("puzzles, places and story, in the designer.") },
            { title: t.t("Test play and publish it"), text: t.t("once the platform admin lists it in the catalog, other organizations can run it.") },
            { title: t.t("Run your own events"), text: t.t("schedule one from any of your adventures, like any organization.") },
          ]
        : [
            CATALOG_ENABLED
              ? { title: t.t("Browse adventures"), text: t.t("the catalog has ready-made adventures, or you can author your own.") }
              : { title: t.t("Author an adventure"), text: t.t("puzzles, places and story, in the designer.") },
            { title: t.t("Get an event pass"), text: t.t("one per event, covering all its players. The first {{n}} play free.", { n: pricing(user).free }) },
            { title: t.t("Schedule an event"), text: t.t("add teams, share magic links, and start when ready.") },
          ];
  const intro =
    variant === "admin"
      ? t.t("Admin shortcut — three steps to a working deployment:")
      : variant === "publisher"
        ? t.t("Your organization publishes the catalog other organizations run. Three steps:")
        : t.t("Three steps from here to a live adventure:");
  const footnote = variant === "admin" ? t.t("The dashboard checklist tracks these steps until the first event runs.") : t.t("You can open this again from Settings → Account → Help.");
  const go: { label: string; route: Route } =
    variant === "admin"
      ? { label: t.t("Manage organizations"), route: { name: "organizations" } }
      : variant === "publisher"
        ? { label: t.t("Open your adventures"), route: { name: "adventures" } }
        : CATALOG_ENABLED
          ? { label: t.t("Browse the catalog"), route: { name: "catalog" } }
          : { label: t.t("Open your adventures"), route: { name: "adventures" } };

  return (
    <Dialog
      title={t.t("Welcome to Zonventure")}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Skip")}</Button>
          <a className="btn primary" href={href(go.route)} onClick={onClose}>{go.label}</a>
        </>
      }
    >
      <p className="db-muted">{intro}</p>
      <ol className="db-welcome">
        {steps.map((s) => (
          <li key={s.title}>
            <strong>{s.title}</strong> — {s.text}
          </li>
        ))}
      </ol>
      <p className="db-muted db-small">{footnote}</p>
    </Dialog>
  );
}
