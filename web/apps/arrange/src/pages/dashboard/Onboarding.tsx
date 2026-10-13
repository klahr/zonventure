import { useEffect, useState } from "react";
import { canAuthor, siteName, type Whoami } from "@zonventure/api";
import { WIDE, wideSrcSet } from "@zonventure/ui";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, href, navigate, type Route } from "../../router.ts";
import { Button, Dialog } from "../../ui/index.ts";
import { activeMembership, pricing } from "../events/api.ts";
import { account } from "../settings/account-api.ts";

export type OnboardingVariant = "arranger" | "publisher" | "admin";

/** Set on the way to Adventures to open the tutorial there. */
export const START_TUTORIAL = "zv-start-tutorial";

export function onboardingVariant(user: Whoami): OnboardingVariant {
  if (user.role === "admin") return "admin";
  return CATALOG_ENABLED && activeMembership(user)?.is_publisher ? "publisher" : "arranger";
}

/**
 * The one-time welcome after signing up (an event host instead lands
 * on Events, once). It is marked shown on the account the moment it
 * opens, so it never comes back on any device — Settings → Account →
 * Help can open OnboardingDialog again. localStorage still counts, for
 * those who saw it before the account remembered.
 */
export function Onboarding({ user }: { user: Whoami }) {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    if (user.welcome_seen) return;
    const key = "zv_onboarded_" + user.id;
    try {
      if (localStorage.getItem(key) === "1") {
        void account.welcomeSeen().catch(() => {});
        return;
      }
    } catch {
      /* storage blocked: the account remembers */
    }
    const timer = setTimeout(() => {
      // A reset or email-change link is a task of its own; don't cover it.
      if (/^#\/?(reset|change-email)/.test(location.hash)) return;
      try {
        localStorage.setItem(key, "1");
      } catch {
        /* shown this once anyway */
      }
      void account.welcomeSeen().catch(() => {});
      // An event host neither authors nor buys passes: no welcome for
      // those tasks, just their first stop, the events.
      if (user.role === "org_member") {
        navigate({ name: "events" });
        return;
      }
      // A writer only authors: their first stop is the adventures.
      if (user.role === "org_writer") {
        navigate({ name: "adventures" });
        return;
      }
      setOpen(true);
    }, 200);
    return () => clearTimeout(timer);
  }, [user.id, user.role, user.welcome_seen]);
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
          { title: t.t("Add a user to it"), text: t.t("an admin can invite team members and run events.") },
          ...(CATALOG_ENABLED ? [{ title: t.t("Choose a catalog publisher"), text: t.t("the organization that authors the catalog; you list and commission its adventures.") }] : []),
          { title: t.t("Turn on public sign-up"), text: t.t("off on a new server; turn it on when anyone may sign up and create an organization.") },
        ]
      : variant === "publisher"
        ? [
            { title: t.t("Author an adventure"), text: t.t("puzzles, places and story, in the designer.") },
            { title: t.t("Test play and publish it"), text: t.t("once the platform admin lists it in the catalog, other organizations can run it.") },
            { title: t.t("Run your own events"), text: t.t("schedule one from any of your adventures, like any organization.") },
          ]
        : [
            !canAuthor(user)
              ? { title: t.t("Pick an adventure"), text: t.t("your organization's admins write the adventures; you run events with them.") }
              : CATALOG_ENABLED
                ? { title: t.t("Browse adventures"), text: t.t("the catalog has ready-made adventures, or you can author your own.") }
                : { title: t.t("Author an adventure"), text: t.t("puzzles, places and story, in the designer.") },
            { title: t.t("Get an event pass"), text: t.t("one per event, covering all its players. The first {{n}} play free.", { n: pricing(user).free }) },
            { title: t.t("Schedule an event"), text: t.t("add teams, share magic links, and start when ready.") },
          ];
  const intro =
    variant === "admin"
      ? CATALOG_ENABLED ? t.t("Admin shortcut — four steps to a working deployment:") : t.t("Admin shortcut — three steps to a working deployment:")
      : variant === "publisher"
        ? t.t("Your organization publishes the catalog other organizations run. Three steps:")
        : t.t("Three steps from here to a live adventure:");
  const footnote = variant === "admin" ? t.t("The dashboard checklist tracks these steps until the first event runs.") : t.t("You can open this again from Settings → Account → Help.");
  // Nothing else works until the seeded password is changed.
  const go: { label: string; route: Route } = user.password_is_default
    ? { label: t.t("Change password"), route: { name: "settings", tab: "account" } }
    : variant === "admin"
      ? { label: t.t("Manage organizations"), route: { name: "organizations" } }
      : variant === "publisher"
        ? { label: t.t("Open your adventures"), route: { name: "adventures" } }
        : CATALOG_ENABLED
          ? { label: t.t("Browse the catalog"), route: { name: "catalog" } }
          : { label: t.t("Open your adventures"), route: { name: "adventures" } };

  return (
    <Dialog
      title={t.t("Welcome to {{name}}", { name: siteName() })}
      onClose={onClose}
      banner={<img src={WIDE.welcome.small} srcSet={wideSrcSet(WIDE.welcome)} sizes="32rem" alt="" width={800} height={267} />}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Skip")}</Button>
          {variant !== "admin" && canAuthor(user) && (
            <a
              className="btn"
              href={href({ name: "adventures" })}
              onClick={() => {
                // The adventures page opens the tutorial's wizard on arrival.
                try {
                  sessionStorage.setItem(START_TUTORIAL, "1");
                } catch {
                  /* storage blocked: the adventures page still offers it */
                }
                onClose();
              }}
            >
              {t.t("Follow the tutorial")}
            </a>
          )}
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
