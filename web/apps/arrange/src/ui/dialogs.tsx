import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Button, ConfirmModal, Modal } from "../designer/dz/ui.tsx";
import { useT } from "../i18n.tsx";

/**
 * A dialog in the designer's style: the shared Modal, sized by wide and
 * huge. With onSubmit the body is a form, so Enter submits; the footer is
 * the page's own buttons.
 */
export function Dialog(props: {
  title: ReactNode;
  icon?: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  onSubmit?: () => void;
  wide?: boolean;
  /** Wider still, for a guide with a preview beside it. */
  huge?: boolean;
  /** A picture across the top, edge to edge, above the title. */
  banner?: ReactNode;
}) {
  const { wide, huge, ...rest } = props;
  return <Modal {...rest} size={huge ? "xl" : wide ? "lg" : "md"} />;
}

interface ConfirmOpts {
  title: string;
  message?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  /** Password dialog only: why the last try failed, shown under the field. */
  error?: string;
}

interface Dialogs {
  /** Ask yes/no. Cancel has focus, so an accidental Enter keeps things as they were. */
  confirm: (o: ConfirmOpts) => Promise<boolean>;
  /**
   * Ask for the user's password before a destructive action; the server
   * checks it (X-Confirm-Password). Resolves null on cancel.
   */
  password: (o: ConfirmOpts) => Promise<string | null>;
}

const Ctx = createContext<Dialogs | null>(null);

type Open =
  | { kind: "confirm"; o: ConfirmOpts; done: (v: boolean) => void }
  | { kind: "password"; o: ConfirmOpts; done: (v: string | null) => void };

export function DialogProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<Open | null>(null);
  const confirm = useCallback((o: ConfirmOpts) => new Promise<boolean>((done) => setOpen({ kind: "confirm", o, done })), []);
  const password = useCallback((o: ConfirmOpts) => new Promise<string | null>((done) => setOpen({ kind: "password", o, done })), []);
  return (
    <Ctx.Provider value={{ confirm, password }}>
      {children}
      {open?.kind === "confirm" && <ConfirmDialog o={open.o} onDone={(v) => { setOpen(null); open.done(v); }} />}
      {open?.kind === "password" && <PasswordDialog o={open.o} onDone={(v) => { setOpen(null); open.done(v); }} />}
    </Ctx.Provider>
  );
}

export function useDialogs(): Dialogs {
  const d = useContext(Ctx);
  if (!d) throw new Error("useDialogs outside DialogProvider");
  return d;
}

function ConfirmDialog({ o, onDone }: { o: ConfirmOpts; onDone: (v: boolean) => void }) {
  return (
    <ConfirmModal
      title={o.title}
      message={o.message}
      confirmLabel={o.confirmLabel}
      {...(o.cancelLabel ? { cancelLabel: o.cancelLabel } : {})}
      {...(o.danger ? { danger: true } : {})}
      onConfirm={() => onDone(true)}
      onCancel={() => onDone(false)}
    />
  );
}

function PasswordDialog({ o, onDone }: { o: ConfirmOpts; onDone: (v: string | null) => void }) {
  const t = useT();
  const [pw, setPw] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  return (
    <Dialog
      title={o.title}
      onClose={() => onDone(null)}
      onSubmit={() => pw && onDone(pw)}
      footer={
        <>
          <Button onClick={() => onDone(null)}>{o.cancelLabel ?? t.t("Cancel")}</Button>
          <Button type="submit" variant={o.danger ? "danger" : "primary"} filled={!!o.danger} disabled={!pw}>{o.confirmLabel}</Button>
        </>
      }
    >
      {o.message && <p className="confirm-message">{o.message}</p>}
      <div className="sf-field">
        <div className="label-row"><label htmlFor="cs-confirm-pw">{t.t("Your password")}</label></div>
        <input id="cs-confirm-pw" ref={input} type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} {...(o.error ? { "aria-invalid": true, "aria-describedby": "cs-confirm-pw-err" } : {})} />
        {o.error && <span id="cs-confirm-pw-err" className="cs-field__error" role="alert">{o.error}</span>}
      </div>
    </Dialog>
  );
}
