import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Button, DzPortal, useEscapeToClose } from "../designer/dz/ui.tsx";
import { useT } from "../i18n.tsx";

/**
 * A dialog in the designer's style. With onSubmit the body is a form, so
 * Enter submits; the footer is the page's own buttons.
 */
export function Dialog(props: {
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  onSubmit?: () => void;
  wide?: boolean;
  /** Wider still, for a guide with a preview beside it. */
  huge?: boolean;
}) {
  const id = useId();
  useEscapeToClose(props.onClose);
  // Focus goes into the dialog — to a field marked autoFocus, else the
  // first field, else the dialog — and back to where it was on close.
  const box = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const el = box.current;
    if (el && !el.contains(document.activeElement)) {
      const first = el.querySelector<HTMLElement>("[autofocus], input:not([type=hidden]):not([disabled]), select, textarea");
      (first ?? el).focus();
    }
    return () => before?.focus?.();
  }, []);
  const Body = props.onSubmit ? "form" : "div";
  return (
    <DzPortal>
      <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
        <Body
          ref={(el: HTMLElement | null) => { box.current = el; }}
          tabIndex={-1}
          className={"modal cs-dialog" + (props.wide ? " cs-dialog--wide" : "") + (props.huge ? " cs-dialog--huge" : "")}
          role="dialog"
          aria-modal="true"
          aria-labelledby={id}
          {...(props.onSubmit ? { onSubmit: (e: FormEvent) => { e.preventDefault(); props.onSubmit!(); } } : {})}
        >
          <div className="modal-head">
            <h2 id={id}>{props.title}</h2>
          </div>
          <div className="modal-body">{props.children}</div>
          {props.footer && <div className="modal-foot">{props.footer}</div>}
        </Body>
      </div>
    </DzPortal>
  );
}

interface ConfirmOpts {
  title: string;
  message?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
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
  const t = useT();
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => cancel.current?.focus(), []);
  return (
    <Dialog
      title={o.title}
      onClose={() => onDone(false)}
      footer={
        <>
          <Button ref={cancel} onClick={() => onDone(false)}>{o.cancelLabel ?? t.t("Cancel")}</Button>
          <Button variant={o.danger ? "danger" : "primary"} filled={!!o.danger} onClick={() => onDone(true)}>{o.confirmLabel}</Button>
        </>
      }
    >
      {o.message && <p className="confirm-message">{o.message}</p>}
    </Dialog>
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
        <input id="cs-confirm-pw" ref={input} type="password" autoComplete="current-password" value={pw} onChange={(e) => setPw(e.target.value)} />
      </div>
    </Dialog>
  );
}
