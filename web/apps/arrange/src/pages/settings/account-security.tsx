import { useEffect, useRef, useState, type ReactNode } from "react";
import { Copy, Download, ShieldCheck, ShieldOff } from "lucide-react";
import type { Whoami } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, Dialog, ErrorBox, Field, HelpPopover, ICON, Pill, errorText, fmtDateTime, relTime, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { MoreMenu } from "../event/Menu.tsx";
import { account, saveBlob, type TotpSetup } from "./account-api.ts";

type Status = { tone: "muted" | "ok" | "err"; text: string } | null;

export function StatusLine({ status }: { status: Status }) {
  return (
    <p className={status?.tone === "err" ? "acct-err" : status?.tone === "ok" ? "acct-ok" : "acct-muted"} role="status" aria-live="polite" hidden={!status}>
      {status?.text}
    </p>
  );
}

/** Copy to the clipboard, falling back to selecting the field for a manual copy. */
export async function copyText(text: string, field: HTMLInputElement | null): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    field?.focus();
    field?.select();
    return false;
  }
}

/** One line of a card's list: what it is, its state, its action, and anything that opens under it. */
export function Item({ name, meta, action, children }: { name: ReactNode; meta?: ReactNode; action?: ReactNode; children?: ReactNode }) {
  return (
    <div className="acct-item acct-item--wrap">
      <div className="acct-item__text">
        <span className="acct-item__name">{name}</span>
        {meta && <span className="acct-item__meta">{meta}</span>}
      </div>
      {action && <div className="acct-item__act">{action}</div>}
      {children && <div className="acct-item__more">{children}</div>}
    </div>
  );
}

/**
 * How the user signs in — password, email, two-factor — one row each,
 * the forms in dialogs. A seeded default password is the exception: its
 * form stays open in the card and takes focus, since the banner on
 * every page sends the user here.
 */
export function SignInCard({ user }: { user: Whoami }) {
  const t = useT();
  const isDefault = !!user.password_is_default;
  return (
    <Card title={t.t("Sign-in")} id="acct-password" {...(isDefault ? { tone: "danger" as const } : {})}>
      <div className="acct-list">
        <PasswordRow isDefault={isDefault} />
        <EmailRow user={user} />
        <TotpRow />
      </div>
    </Card>
  );
}

/** Change password; doing so signs out every other session. */
function usePasswordForm(onDone: () => void, focus?: boolean) {
  const t = useT();
  const toast = useToast();
  const { refresh } = useSession();
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const curRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!focus) return;
    curRef.current?.scrollIntoView({ block: "center" });
    curRef.current?.focus({ preventScroll: true });
  }, [focus]);

  const submit = async () => {
    if (next.length < 8) return setError(t.t("New password must be at least 8 characters."));
    setBusy(true);
    setError(null);
    try {
      await account.changePassword(cur, next);
      setCur("");
      setNext("");
      toast.ok(t.t("Password updated. You're still signed in here; every other session was kicked."));
      // A fresh whoami clears password_is_default and its banner.
      await refresh();
      onDone();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const fields = (
    <>
      <Field label={t.t("Current password")}>
        <input ref={curRef} type="password" autoComplete="current-password" required value={cur} onChange={(e) => setCur(e.target.value)} />
      </Field>
      <Field label={t.t("New password (min 8)")} error={error}>
        <input type="password" autoComplete="new-password" minLength={8} required value={next} onChange={(e) => setNext(e.target.value)} />
      </Field>
    </>
  );
  const button = <Button type="submit" variant="primary" loading={busy}>{t.t("Update password")}</Button>;
  return { fields, button, submit };
}

function PasswordRow({ isDefault }: { isDefault: boolean }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const inline = usePasswordForm(() => {}, isDefault);
  if (isDefault) {
    return (
      <Item name={t.t("Password")} meta={<span className="acct-err">{t.t("Still the seeded default — change it now.")}</span>}>
        <form className="acct-form" onSubmit={(e) => { e.preventDefault(); void inline.submit(); }}>
          {inline.fields}
          <div>{inline.button}</div>
        </form>
      </Item>
    );
  }
  return (
    <Item
      name={t.t("Password")}
      meta={t.t("Changing it signs out every other session.")}
      action={<Button size="sm" onClick={() => setOpen(true)}>{t.t("Change password")}</Button>}
    >
      {open && <PasswordDialog onClose={() => setOpen(false)} />}
    </Item>
  );
}

function PasswordDialog({ onClose }: { onClose: () => void }) {
  const t = useT();
  const form = usePasswordForm(onClose);
  return (
    <Dialog title={t.t("Change password")} onClose={onClose} onSubmit={() => void form.submit()} footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button>{form.button}</>}>
      {form.fields}
    </Dialog>
  );
}

/**
 * The email address: a change sends a confirmation link to the new
 * address, and for 7 days after confirming it can be reverted, in case
 * the new address was mistyped.
 */
function EmailRow({ user }: { user: Whoami }) {
  const t = useT();
  const toast = useToast();
  const { confirm } = useDialogs();
  const { refresh } = useSession();
  const revert = useLoad((s) => account.emailRevertStatus(s), [user.email]);
  const [open, setOpen] = useState(false);

  const doRevert = async (prev: string) => {
    if (!(await confirm({ title: t.t("Revert email change"), message: t.t("Revert your email back to {{prev}}?", { prev }), confirmLabel: t.t("Revert email change") }))) return;
    try {
      const r = await account.revertEmail();
      toast.ok(t.t("Email reverted to {{email}}.", { email: r?.email || prev }));
      await refresh();
      revert.reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const rs = revert.data;
  return (
    <Item
      name={t.t("Email")}
      meta={
        user.email ? (
          <>
            {user.email} {user.email_verified ? <Pill tone="good">{t.t("verified")}</Pill> : <Pill tone="warn">{t.t("unverified")}</Pill>}
          </>
        ) : (
          t.t("(none — admin-seeded account)")
        )
      }
      action={
        <Button size="sm" id="acct-email-open" onClick={() => setOpen(true)}>
          {user.email ? t.t("Change email") : t.t("Add email")}
        </Button>
      }
    >
      {rs?.available && rs.previous_email && (
        <Callout variant="warning">
          <p style={{ margin: 0 }}>{t.t("Recent change from {{prev}} → {{cur}}.", { prev: rs.previous_email, cur: user.email || "—" })}</p>
          <p className="acct-muted" style={{ margin: "4px 0 8px" }}>
            {t.t("You can revert until {{when}} if you typed the new address wrong.", { when: fmtDateTime(rs.expires_at, t.current()) })}
          </p>
          <Button size="sm" onClick={() => void doRevert(rs.previous_email!)}>{t.t("Revert email change")}</Button>
        </Callout>
      )}
      {open && <EmailDialog title={user.email ? t.t("Change email") : t.t("Add email")} onClose={() => setOpen(false)} />}
    </Item>
  );
}

function EmailDialog({ title, onClose }: { title: string; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const [email, setEmail] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const to = email.trim();
    if (!to) return;
    setBusy(true);
    setError(null);
    try {
      await account.changeEmail(to);
      toast.ok(t.t("Confirmation link sent to {{email}}.", { email: to }));
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Send confirmation")}</Button></>}
    >
      <Field
        label={t.t("New email")}
        error={error}
        hint={t.t("We send a confirmation link to the new address — your email only changes when you click it. After confirming you have a {{n}}-day window to revert if you typed it wrong.", { n: 7 })}
      >
        <input id="acct-new-email" type="email" autoComplete="email" autoFocus required value={email} onChange={(e) => setEmail(e.target.value)} />
      </Field>
    </Dialog>
  );
}

/**
 * Two-factor authentication. Three states: the server has no
 * ZV_TOTP_KEY (enrolment impossible), enrolled (regenerate recovery
 * codes, disable — both re-check the password), or not enrolled (set
 * up: QR and secret, then a code to confirm, then recovery codes).
 */
function TotpRow() {
  const t = useT();
  const toast = useToast();
  const { password } = useDialogs();
  const { refresh } = useSession();
  const status = useLoad((s) => account.totp(s), []);
  const [setup, setSetup] = useState<TotpSetup | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const name = t.t("Two-factor authentication");

  const start = async () => {
    setBusy(true);
    try {
      setSetup(await account.totpSetup());
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };

  const regenerate = async () => {
    const pw = await password({
      title: t.t("Regenerate recovery codes"),
      message: t.t("Confirm your current password to regenerate recovery codes:"),
      confirmLabel: t.t("Regenerate recovery codes"),
    });
    if (!pw) return;
    try {
      const r = await account.totpRegenerate(pw);
      toast.ok(t.t("New recovery codes generated. Save them now — old codes no longer work."));
      setCodes(r?.recovery_codes ?? []);
    } catch (e) {
      toast.err(e);
    }
  };

  const disable = async () => {
    const pw = await password({
      title: t.t("Disable 2FA"),
      message: t.t("Turn off two-factor authentication for this account?") + " " + t.t("Confirm your current password to disable 2FA:"),
      confirmLabel: t.t("Disable 2FA"),
      danger: true,
    });
    if (!pw) return;
    try {
      await account.totpDisable(pw);
      toast.ok(t.t("Two-factor authentication disabled."));
      await refresh();
      status.reload();
    } catch (e) {
      toast.err(e);
    }
  };

  const s = status.data;
  const dialogs = (
    <>
      {setup && (
        <TotpSetupDialog
          setup={setup}
          onClose={() => setSetup(null)}
          onDone={async (c) => {
            setSetup(null);
            setCodes(c);
            await refresh();
            status.reload();
          }}
        />
      )}
      {codes && <RecoveryCodesDialog codes={codes} onClose={() => setCodes(null)} />}
    </>
  );
  let row;
  if (!s) {
    row = (
      <Item name={name} meta={status.error ? undefined : t.t("Loading…")}>
        {status.error && <ErrorBox message={status.error} retry={status.reload} retryLabel={t.t("Retry")} />}
      </Item>
    );
  } else if (!s.server_ready) {
    row = (
      <Item
        name={name}
        meta={
          <>
            {t.t("Not available on this server.")}{" "}
            <HelpPopover label={t.t("Why not?")} body={t.t("TOTP is not configured on this server. Ask your operator to set $ZV_TOTP_KEY (32 bytes base64) to enable enrolment.")} />
          </>
        }
      />
    );
  } else if (s.enabled) {
    row = (
      <Item
        name={<><ShieldCheck size={ICON.sm} aria-hidden className="acct-good" />{name}</>}
        meta={s.enrolled_at ? <span title={fmtDateTime(s.enrolled_at, t.current())}>{t.t("On since {{when}}", { when: relTime(s.enrolled_at, t.current()) })}</span> : t.t("On")}
        action={
          <MoreMenu
            label={t.t("Manage")}
            items={[
              { label: t.t("Regenerate recovery codes"), onClick: () => void regenerate() },
              { label: t.t("Disable 2FA"), icon: <ShieldOff size={ICON.sm} aria-hidden />, danger: true, onClick: () => void disable() },
            ]}
          />
        }
      />
    );
  } else {
    row = (
      <Item
        name={name}
        meta={
          <>
            {t.t("Off — a code from an authenticator app on top of your password.")}{" "}
            <HelpPopover
              label={t.t("About two-factor authentication")}
              body={t.t("Add a second factor so a stolen password alone cannot log into your account. Requires an authenticator app like 1Password, Bitwarden, Aegis, Google Authenticator or similar.")}
            />
          </>
        }
        action={<Button size="sm" loading={busy} onClick={() => void start()}>{t.t("Set up 2FA")}</Button>}
      />
    );
  }
  return <>{row}{dialogs}</>;
}

function TotpSetupDialog({ setup, onClose, onDone }: { setup: TotpSetup; onClose: () => void; onDone: (codes: string[]) => Promise<void> }) {
  const t = useT();
  const toast = useToast();
  const [code, setCode] = useState("");
  const [status, setStatus] = useState<Status>(null);
  const [busy, setBusy] = useState(false);
  const secretRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);

  const submit = async () => {
    if (code.length !== 6) return setStatus({ tone: "err", text: t.t("Enter all 6 digits.") });
    setBusy(true);
    setStatus({ tone: "muted", text: t.t("Confirming…") });
    try {
      const r = await account.totpConfirm(code);
      await onDone(r?.recovery_codes ?? []);
    } catch (e) {
      setStatus({ tone: "err", text: errorText(e) });
      setCode("");
      codeRef.current?.focus();
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={t.t("Set up 2FA")}
      onClose={onClose}
      onSubmit={() => void submit()}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Confirm and enable")}</Button></>}
    >
      <p className="acct-muted">{t.t("Scan this QR code in your authenticator app, or enter the secret manually.")}</p>
      {setup.qr_data_url && <img className="acct-qr" src={setup.qr_data_url} alt={t.t("TOTP QR code")} />}
      <Field label={t.t("Secret")}>
        <div className="acct-copy">
          <input ref={secretRef} readOnly value={setup.secret_base32} onFocus={(e) => e.currentTarget.select()} />
          <Button
            size="sm"
            icon={<Copy size={14} aria-hidden />}
            title={t.t("Copy secret")}
            onClick={async () => {
              if (await copyText(setup.secret_base32, secretRef.current)) toast.ok(t.t("Secret copied"));
              else toast.err(t.t("Copy failed — select the field and copy manually."));
            }}
          >
            {t.t("Copy")}
          </Button>
        </div>
      </Field>
      <Field label={t.t("Then enter the 6-digit code your app shows to confirm:")}>
        <input
          ref={codeRef}
          className="acct-otp"
          autoFocus
          value={code}
          onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
          autoComplete="one-time-code"
          inputMode="numeric"
          pattern="[0-9]{6}"
          maxLength={6}
          required
        />
      </Field>
      <StatusLine status={status} />
    </Dialog>
  );
}

/**
 * Freshly minted recovery codes. They are never shown again, so the
 * dialog only closes on an explicit "I saved them" — not on Escape or
 * a click outside.
 */
function RecoveryCodesDialog({ codes, onClose }: { codes: string[]; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const first = useRef<HTMLButtonElement>(null);
  useEffect(() => first.current?.focus(), []);
  return (
    <Dialog
      title={t.t("Save your recovery codes")}
      onClose={() => {}}
      footer={<Button variant="primary" onClick={onClose}>{t.t("I saved them")}</Button>}
    >
      <p className="confirm-message">{t.t("Store these somewhere safe. Each code works once if you lose access to your authenticator.")}</p>
      <ul className="acct-codes" aria-label={t.t("Recovery codes")}>
        {codes.map((c) => <li key={c}>{c}</li>)}
      </ul>
      <div className="cs-row cs-row--sm">
        <Button
          ref={first}
          icon={<Download size={14} aria-hidden />}
          onClick={() => saveBlob(new Blob([codes.join("\n") + "\n"], { type: "text/plain" }), "zonventure-recovery-codes.txt")}
        >
          {t.t("Download as .txt")}
        </Button>
        <Button
          icon={<Copy size={14} aria-hidden />}
          onClick={() => {
            navigator.clipboard?.writeText(codes.join("\n")).then(
              () => toast.ok(t.t("Copied.")),
              () => toast.err(t.t("Could not copy.")),
            );
          }}
        >
          {t.t("Copy to clipboard")}
        </Button>
      </div>
    </Dialog>
  );
}
