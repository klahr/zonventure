import { useState } from "react";
import type { Whoami } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { href, navigate, useRoute } from "../../router.ts";
import { Button, errorText, fmtDateTime } from "../../ui/index.ts";
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
  const verify = !!user.email && !user.email_verified;
  const addEmail = !user.email && user.role !== "admin";
  if (!user.scheduled_deletion_at && !verify && !addEmail) return null;
  return (
    <div className="acct-banners">
      {user.scheduled_deletion_at && <DeletionBanner when={user.scheduled_deletion_at} />}
      {verify && <VerifyBanner email={user.email!} />}
      {addEmail && <AddEmailBanner />}
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
  const { refresh } = useSession();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<Status>(null);
  const cancel = async () => {
    setBusy(true);
    setStatus(null);
    try {
      await account.cancelDeletion();
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

function VerifyBanner({ email }: { email: string }) {
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
        <span>{t.t("We sent a link to {{email}}. Until you click it, events and purchases are disabled.", { email })}</span>
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
