import { useState } from "react";
import { Download, Trash2 } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Card, HelpPopover, errorText, fmtDateTime, relTime, useDialogs, useToast } from "../../ui/index.ts";
import { account, download } from "./account-api.ts";
import { Item, StatusLine } from "./account-security.tsx";

type Status = { tone: "muted" | "ok" | "err"; text: string } | null;

/**
 * The account's downloads: the GDPR export — one JSON file of the
 * account and its organisation's data — and, for org admins, the org's
 * portable migration bundle, re-importable on any instance.
 */
export function DownloadsCard({ bundle, admin }: { bundle: boolean; admin: boolean }) {
  const t = useT();
  return (
    <Card title={t.t("Download your data")}>
      <div className="acct-list">
        <ExportRow admin={admin} />
        {bundle && <BundleRow />}
      </div>
    </Card>
  );
}

function ExportRow({ admin }: { admin: boolean }) {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const inOrg = state.status === "authenticated" && state.user.memberships.length > 0;
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setDone(false);
    try {
      const name = await download("/api/admin/me/export", "zonventure-export.json");
      toast.ok(t.t("Downloaded {{name}}.", { name }));
      setDone(true);
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Item
      name={t.t("My data")}
      meta={admin || !inOrg ? t.t("A single JSON file with your account data.") : t.t("A single JSON file with your account data plus your organization's events, teams, adventures, event passes and purchases.")}
      action={<Button size="sm" icon={<Download size={14} aria-hidden />} loading={busy} onClick={() => void run()}>{t.t("Download")}</Button>}
    >
      {/* Right-to-erasure follow-up: with the data in hand, deletion is one step away. */}
      {done && (
        <p className="cs-note">
          <a
            href="#acct-delete"
            onClick={(e) => {
              e.preventDefault();
              document.getElementById("acct-delete")?.scrollIntoView({ behavior: "smooth", block: "center" });
            }}
          >
            {t.t("Want to delete your account too? →")}
          </a>
        </p>
      )}
    </Item>
  );
}

function BundleRow() {
  const t = useT();
  const toast = useToast();
  const [status, setStatus] = useState<Status>(null);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setStatus({ tone: "muted", text: t.t("Preparing bundle — this can take a moment for large orgs.") });
    try {
      const name = await download("/api/admin/migrate/self-export", "zonventure-org-bundle.zip");
      setStatus(null);
      toast.ok(t.t("Downloaded {{name}}.", { name }));
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Item
      name={t.t("Migration bundle")}
      meta={
        <>
          {t.t("A zip of your whole organization, re-importable on any Zonventure instance.")}{" "}
          <HelpPopover
            label={t.t("About the migration bundle")}
            body={
              t.t("A portable zip containing your organization — adventures, events, teams, slots, ticket purchases, audit log. The same format the Migration wizard uses to move an org between instances. Re-importable on any Zonventure instance.") +
              " " +
              t.t("Tokens (team / recap / live / slot) are preserved verbatim, so existing magic-link emails and printed QR codes keep working on the destination.")
            }
          />
        </>
      }
      action={<Button size="sm" icon={<Download size={14} aria-hidden />} loading={busy} onClick={() => void run()}>{t.t("Download")}</Button>}
    >
      {status && <StatusLine status={status} />}
    </Item>
  );
}

/**
 * Delete the account, after a 7-day grace period. Scheduling drops
 * every session, so the page goes back to the login screen; logging
 * back in within the week shows the banner to cancel. While a deletion
 * is scheduled the card offers the cancel itself.
 */
export function DeleteCard({ user }: { user: Whoami }) {
  const t = useT();
  const toast = useToast();
  const { password } = useDialogs();
  const { refresh } = useSession();
  const [status, setStatus] = useState<Status>(null);
  const [busy, setBusy] = useState(false);
  const loc = t.current();

  const schedule = async () => {
    const orgs = user.memberships.filter((m) => m.sole_member).map((m) => m.org_name).join(", ");
    const pw = await password({
      title: t.t("Schedule account deletion?"),
      message:
        t.t("Type your password to confirm. You'll be logged out, and have 7 days to log back in and tap Cancel deletion.") +
        (orgs ? " " + t.t("You're the only member of {{orgs}}, so it is deleted with you: its events, teams, adventures and event passes.", { orgs }) : ""),
      confirmLabel: t.t("Schedule deletion"),
      danger: true,
    });
    if (!pw) return;
    setBusy(true);
    setStatus({ tone: "muted", text: t.t("Scheduling…") });
    try {
      const r = await account.scheduleDeletion(pw);
      // A hash-only change doesn't reload: reload so the signed-out
      // console (the server dropped every session) shows the notice.
      location.replace(location.pathname + "#/deletion-scheduled?at=" + encodeURIComponent(r.scheduled_deletion_at));
      location.reload();
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) });
      setBusy(false);
    }
  };

  const cancel = async () => {
    setBusy(true);
    try {
      await account.cancelDeletion();
      toast.ok(t.t("Account-deletion cancelled."));
      await refresh();
    } catch (e) {
      toast.err(errorText(e) || t.t("Could not cancel — try again."));
    } finally {
      setBusy(false);
    }
  };

  const when = user.scheduled_deletion_at;
  return (
    <Card title={t.t("Delete account")} tone="danger" id="acct-delete">
      <p className="cs-note">
        {when ? (
          <span title={fmtDateTime(when, loc)}>{t.t("Scheduled — your account is deleted {{when}}. You can cancel until then.", { when: relTime(when, loc) })}</span>
        ) : (
          t.t("Deleted after a 7-day grace period. To cancel, log back in before then and tap Cancel deletion.")
        )}{" "}
        <HelpPopover
          label={t.t("What gets deleted")}
          body={t.t("Schedules your account for deletion in 7 days. You can cancel any time before then: log back in and tap Cancel deletion. After the grace period, your account and (if you're the last member) your organization — runs, teams, adventures, purchases — are permanently removed.")}
        />
      </p>
      <StatusLine status={status} />
      <div>
        {when ? (
          <Button loading={busy} onClick={() => void cancel()}>{t.t("Cancel deletion")}</Button>
        ) : (
          <Button variant="danger" icon={<Trash2 size={14} aria-hidden />} loading={busy} onClick={() => void schedule()}>{t.t("Schedule deletion…")}</Button>
        )}
      </div>
    </Card>
  );
}
