import { useState } from "react";
import { post, readsAsAdmin, type Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href, navigate, useRoute } from "../../router.ts";
import { Button, errorText, fmtDateTime, useDialogs, useToast } from "../../ui/index.ts";
import { account } from "./account-api.ts";
import "./account.css";

/**
 * The account-wide strips above every page, as in the classic console:
 * a scheduled deletion (outranks the rest, it affects the whole
 * account), then an unverified email — money-touching actions refuse
 * until it is verified — or, for an account with no email at all, the
 * way to add one. Global admins run no events, so they are spared the
 * email nag. These sit outside the toast provider, so each reports its
 * own result inline.
 */
export function AccountBanners({ user }: { user: Whoami }) {
  const route = useRoute();
  if (route.name === "designer") return null;
  // Until the forced password change or 2FA setup is done the server
  // refuses the rest, and the settings page shows only that setup.
  const setupOnly = !!user.password_is_default || !!user.totp_setup_required;
  const verify = !!user.email && !user.email_verified;
  const addEmail = !user.email && !readsAsAdmin(user) && !setupOnly;
  const invitations = setupOnly ? [] : (user.org_invitations ?? []);
  const ownPassword = !!user.password_set_by_admin && !setupOnly;
  if (!user.scheduled_deletion_at && !verify && !addEmail && invitations.length === 0 && !ownPassword) return null;
  return (
    <div className="acct-banners">
      {user.scheduled_deletion_at && <DeletionBanner when={user.scheduled_deletion_at} />}
      {invitations.map((inv) => <InvitationBanner key={inv.org_id} orgID={inv.org_id} orgName={inv.org_name} role={inv.role} />)}
      {ownPassword && <OwnPasswordBanner />}
      {verify && <VerifyBanner email={user.email!} lobby={user.memberships.length === 0} />}
      {addEmail && <AddEmailBanner />}
    </div>
  );
}

/** An organisation asked to add this account; nothing changes until they answer. */
function InvitationBanner({ orgID, orgName, role }: { orgID: number; orgName: string; role: string }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const { refresh } = useSession();
  const [busy, setBusy] = useState<"accept" | "decline" | null>(null);
  const [status, setStatus] = useState<Status>(null);
  const answer = async (how: "accept" | "decline") => {
    if (how === "decline") {
      const go = await dialogs.confirm({
        title: t.t("Decline the invitation to {{name}}?", { name: orgName }),
        message: t.t("You stay out of {{name}}. To join later, an admin there has to invite you again.", { name: orgName }),
        confirmLabel: t.t("Decline"),
      });
      if (!go) return;
    }
    setBusy(how);
    setStatus(null);
    try {
      await post<unknown>(`/api/admin/me/org-invitations/${orgID}/${how}`);
      if (how === "decline") toast.ok(t.t("Invitation to {{name}} declined.", { name: orgName }));
      await refresh();
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) || t.t("Could not answer; try again.") });
      setBusy(null);
    }
  };
  return (
    <div className="acct-banner">
      <div className="acct-banner__text">
        <strong>{t.t("Join {{name}}?", { name: orgName })}</strong> —{" "}
        <span>
          {role === "org_admin"
            ? t.t("{{name}} invites you to join as an admin.", { name: orgName })
            : role === "org_writer"
              ? t.t("{{name}} invites you to join as a writer.", { name: orgName })
              : t.t("{{name}} invites you to join as an event host.", { name: orgName })}
        </span>
      </div>
      <div className="acct-banner__act">
        <StatusText status={status} />
        <Button size="sm" onClick={() => void answer("decline")} loading={busy === "decline"} disabled={busy !== null}>{t.t("Decline")}</Button>
        <Button size="sm" variant="primary" onClick={() => void answer("accept")} loading={busy === "accept"} disabled={busy !== null}>{t.t("Accept")}</Button>
      </div>
    </div>
  );
}

/** An admin chose this password and handed it over; ask for the user's own. */
function OwnPasswordBanner() {
  const t = useT();
  return (
    <div className="acct-banner acct-banner--compact">
      <div className="acct-banner__text">
        <strong>{t.t("Choose your own password")}</strong><span className="acct-banner__more"> — {t.t("An admin set the password you signed in with. Replace it with one only you know.")}</span>
      </div>
      <div className="acct-banner__act">
        <a className="acct-linkbtn" href={href({ name: "settings", tab: "account" })}>{t.t("Change password")}</a>
      </div>
    </div>
  );
}

type Status = { tone: "ok" | "err"; text: string } | null;

function StatusText({ status }: { status: Status }) {
  return (
    <span className={"acct-banner__status" + (status?.tone === "err" ? " is-err" : "")} role="status" aria-live="polite" hidden={!status}>
      {status?.text}
    </span>
  );
}

function DeletionBanner({ when }: { when: string }) {
  const t = useT();
  const toast = useToast();
  const { refresh } = useSession();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<Status>(null);
  const cancel = async () => {
    setBusy(true);
    setStatus(null);
    try {
      await account.cancelDeletion();
      toast.ok(t.t("Account-deletion cancelled."));
      await refresh();
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) || t.t("Could not cancel — try again.") });
      setBusy(false);
    }
  };
  return (
    <div className="acct-banner acct-banner--danger" role="alert">
      <div className="acct-banner__text">
        <strong>{t.t("Account deletion scheduled")}</strong> —{" "}
        <span>{t.t("Your account will be permanently deleted on {{when}}. You can cancel any time before then.", { when: fmtDateTime(when, t.current()) })}</span>
      </div>
      <div className="acct-banner__act">
        <StatusText status={status} />
        <Button size="sm" onClick={() => void cancel()} loading={busy}>{t.t("Cancel deletion")}</Button>
      </div>
    </div>
  );
}

function VerifyBanner({ email, lobby }: { email: string; lobby: boolean }) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<Status>(null);
  const resend = async () => {
    setBusy(true);
    setStatus(null);
    try {
      await account.resendVerification();
      setStatus({ tone: "ok", text: t.t("Verification email re-sent.") });
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) || t.t("Could not resend; try again later.") });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="acct-banner">
      <div className="acct-banner__text">
        <strong>{t.t("Verify your email")}</strong> —{" "}
        {/* Someone waiting to be added to an organization buys and runs nothing yet. */}
        <span>{lobby ? t.t("We sent a link to {{email}}. Click it to confirm your address.", { email }) : t.t("We sent a link to {{email}}. Until you click it, events and purchases are disabled.", { email })}</span>
      </div>
      <div className="acct-banner__act">
        <StatusText status={status} />
        <Button size="sm" onClick={() => void resend()} loading={busy}>{t.t("Resend email")}</Button>
      </div>
    </div>
  );
}

function AddEmailBanner() {
  const t = useT();
  const target = href({ name: "settings", tab: "account" });
  return (
    <div className="acct-banner">
      <div className="acct-banner__text">
        <strong>{t.t("Add your email")}</strong> — <span>{t.t("Events and purchases need a verified email address.")}</span>
      </div>
      <div className="acct-banner__act">
        <a
          className="acct-linkbtn"
          href={target}
          onClick={(e) => {
            e.preventDefault();
            navigate({ name: "settings", tab: "account" });
            focusEmailCard();
          }}
        >
          {t.t("Add email")}
        </a>
      </div>
    </div>
  );
}

/** Open the Account tab's email dialog once the tab has rendered; the dialog focuses its field. */
export function focusEmailCard(tries = 20) {
  const open = document.getElementById("acct-email-open");
  if (open) {
    open.scrollIntoView({ behavior: "smooth", block: "center" });
    open.click();
    return;
  }
  if (tries > 0) setTimeout(() => focusEmailCard(tries - 1), 100);
}
