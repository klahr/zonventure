import { useState, type ReactNode } from "react";
import { BookOpen, Monitor, Moon, Plus, Sparkles, Sun, type LucideIcon } from "lucide-react";
import { readsAsAdmin, type Whoami } from "@zonventure/api";
import { readTheme, setTheme, type Theme } from "@zonventure/ui";
import { useSession } from "../../session.tsx";
import { chooseLocale, useT } from "../../i18n.tsx";
import { navigate } from "../../router.ts";
import { Avatar, Button, Card, Dialog, Field, errorText, useToast } from "../../ui/index.ts";
import { account } from "./account-api.ts";
import { SignInCard } from "./account-security.tsx";
import { AuditCard, McpTokensCard, SessionsCard } from "./account-activity.tsx";
import { DeleteCard, DownloadsCard } from "./account-data.tsx";
import { NotificationsCard } from "./account-notifications.tsx";
import { AvatarCard, NameCard } from "./account-avatar.tsx";
import "./account.css";

/**
 * Settings → Account: the user's preferences, sign-in and security, and
 * data, for every role — the classic console's renderSettingsAccount,
 * grouped under headings so the long page scans; forms open in dialogs.
 */
export function AccountTab() {
  const t = useT();
  const { state } = useSession();
  if (state.status !== "authenticated") return null;
  const user = state.user;
  // The server refuses everything but the password change, or the
  // required 2FA setup, meanwhile.
  if (user.password_is_default || user.totp_setup_required) {
    return (
      <div className="acct">
        <Section title={t.t("Sign-in and security")}>
          <SignInCard user={user} />
        </Section>
      </div>
    );
  }
  return (
    <div className="acct">
      <Section title={t.t("Profile and preferences")}>
        <AvatarCard user={user} />
        <NameCard user={user} />
        <RoleCard user={user} />
        <AppearanceCard />
        <HelpCard user={user} />
        {/* Every notification is about authoring adventures, which the platform admin doesn't do. */}
        {!readsAsAdmin(user) && <NotificationsCard />}
      </Section>

      <Section title={t.t("Sign-in and security")}>
        <SignInCard user={user} />
        <div className="acct-span"><SessionsCard /></div>
        <div className="acct-span"><McpTokensCard mcpEnabled={!!user.mcp_enabled} admin={readsAsAdmin(user)} /></div>
        <div className="acct-span"><AuditCard /></div>
      </Section>

      <Section title={t.t("Your data")}>
        {/* The bundle carries answer keys, buyers' emails and slot links: admins only. */}
        <DownloadsCard bundle={user.role === "org_admin"} admin={readsAsAdmin(user)} />
        <DeleteCard user={user} />
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="cs-section" aria-label={title}>
      <h2 className="cs-section__title">{title}</h2>
      <div className="acct-grid">{children}</div>
    </section>
  );
}

/** Not in any organisation yet: signed up without one, or left the last. */
export function isLobby(u: Whoami): boolean {
  return !readsAsAdmin(u) && u.memberships.length === 0;
}

/**
 * Which role the user holds and what it lets them do, who they are
 * signed in as, and the organisation they act for — so nobody has to
 * reverse-engineer their role from the tab strip.
 */
function RoleCard({ user }: { user: Whoami }) {
  const t = useT();
  const [creating, setCreating] = useState(false);
  const lobby = isLobby(user);
  const active = user.memberships.find((m) => m.org_id === user.org_id);
  let label: string, blurb: string;
  if (user.role === "admin") {
    label = t.t("Global admin");
    blurb = t.t("You manage the organizations on this instance, curate the catalog, and keep an eye on the system. Authoring and running events is up to the organizations. Treat this like a root password.");
  } else if (user.role === "support") {
    label = t.t("Support");
    blurb = t.t("You see the organizations, their events and the logs. You can verify a user's email, resend a lost host link or a team's links and reset a user's two-factor sign-in; everything else, such as purchases, pricing and system settings, is up to a global admin.");
  } else if (lobby) {
    label = t.t("No organization yet");
    blurb = t.t("You're signed in but not part of any organization. Create one (you'll be its admin) or wait for an invitation.");
  } else if (user.role === "org_admin") {
    label = t.t("Organization admin");
    blurb = t.t("You can manage members, purchases, branding, and settings for your organization. Run events too.");
  } else if (user.role === "org_writer") {
    label = t.t("Writer");
    blurb = t.t("You can create, edit and publish your organization's adventures. Running events, members and billing are up to others.");
  } else {
    label = t.t("Event host");
    blurb = t.t("You can run events in your organization. Authoring adventures and managing members is reserved for admins.");
  }
  return (
    <Card title={label} subtitle={blurb}>
      <div className="acct-who">
        <span>
          {t.t("Signed in as")} <strong>{user.username}</strong>
        </span>
        {active && (
          <span className="acct-who__org">
            {active.org_logo_url && <Avatar name={active.org_name} src={active.org_logo_url} size={24} />}
            <span>
              {t.t("Acting as ")}
              <strong>{active.org_name}</strong>
            </span>
          </span>
        )}
      </div>
      {/* Admins have their own flow on the Organizations page. */}
      {!readsAsAdmin(user) && (
        <div>
          <Button size="sm" variant={lobby ? "primary" : "secondary"} icon={<Plus size={14} aria-hidden />} onClick={() => setCreating(true)}>
            {lobby ? t.t("Create an organization") : t.t("Create another organization")}
          </Button>
        </div>
      )}
      {creating && <CreateOrgDialog onClose={() => setCreating(false)} />}
    </Card>
  );
}

function CreateOrgDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const { refresh } = useSession();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const n = name.trim();
    if (!n) return setError(t.t("Name required"));
    setBusy(true);
    setError(null);
    try {
      await account.createOrg(n);
      await refresh();
      navigate({ name: "dashboard" });
      toast.ok(t.t('Organization "{{name}}" created — you\'re its admin', { name: n }));
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Create an organization")}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Organization name")} error={error} hint={t.t("You'll be the only member to start with — invite teammates afterwards from Settings.")}>
        <input autoFocus required value={name} onChange={(e) => setName(e.target.value)} placeholder={t.t('e.g. "Acme Field Trips"')} />
      </Field>
    </Dialog>
  );
}

/** Each language in its own words, so a reader can find theirs. */
const LANGUAGE: Record<string, string> = { en: "English", sv: "Svenska" };

/**
 * Theme and language. The account menu at the foot of the sidebar has
 * the same two; both write the same stored preference.
 */
function AppearanceCard() {
  const t = useT();
  const [theme, setThemeState] = useState<Theme>(() => readTheme());
  const themes: { value: Theme; label: string; icon: LucideIcon }[] = [
    { value: "system", label: "System", icon: Monitor },
    { value: "light", label: "Light", icon: Sun },
    { value: "dark", label: "Dark", icon: Moon },
  ];
  return (
    <Card title={t.t("Appearance")}>
      <div className="acct-pref">
        <span className="acct-pref__label" id="acct-theme">{t.t("Theme")}</span>
        <div className="acct-choice" role="radiogroup" aria-labelledby="acct-theme">
          {themes.map((o) => (
            <button
              key={o.value}
              type="button"
              role="radio"
              aria-checked={theme === o.value}
              onClick={() => {
                setTheme(o.value);
                setThemeState(o.value);
              }}
            >
              <o.icon size={14} aria-hidden />
              {t.t(o.label)}
            </button>
          ))}
        </div>
      </div>
      <div className="acct-pref">
        <span className="acct-pref__label" id="acct-lang">{t.t("Language")}</span>
        <div className="acct-choice" role="radiogroup" aria-labelledby="acct-lang">
          {t.available().map((l) => (
            <button key={l} type="button" role="radio" aria-checked={t.current() === l} lang={l} onClick={() => chooseLocale(l)}>
              {LANGUAGE[l] ?? l.toUpperCase()}
            </button>
          ))}
        </div>
      </div>
    </Card>
  );
}

/**
 * The welcome tour again, and the manual. The dashboard shows the tour
 * to a user whose onboarded flag is unset, so clearing it and going
 * there replays it — for whichever tour fits the role.
 */
function HelpCard({ user }: { user: Whoami }) {
  const t = useT();
  const toast = useToast();
  const { refresh } = useSession();
  const again = async () => {
    try {
      localStorage.removeItem("zv_onboarded_" + user.id);
    } catch {
      /* storage blocked: the dashboard will not know it was seen either */
    }
    try {
      await account.forgetWelcomeSeen();
      await refresh();
    } catch (e) {
      toast.err(errorText(e));
      return;
    }
    navigate({ name: "dashboard" });
  };
  return (
    <Card title={t.t("Help")} subtitle={t.t("Step-by-step walk-through of authoring adventures, scheduling events, adding teams, and integrating.")}>
      <div className="cs-row cs-row--sm">
        <Button icon={<Sparkles size={14} aria-hidden />} onClick={() => void again()}>{t.t("Show the welcome again")}</Button>
        <a className="acct-linkbtn" href="/manual" target="_blank" rel="noopener">
          <BookOpen size={14} aria-hidden style={{ marginRight: 6 }} />
          {t.t("Open the manual")}
        </a>
      </div>
    </Card>
  );
}
