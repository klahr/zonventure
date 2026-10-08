import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

export interface ConfirmOptions {
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  /** The action can't be undone: the confirm button is drawn as a warning. */
  danger?: boolean;
}

/**
 * A yes/no question in the page's own style, in place of window.confirm.
 * Render the returned element anywhere; ask resolves true on confirm.
 * Cancel has focus, so an accidental Enter keeps things as they were.
 */
export function useConfirm(): [(o: ConfirmOptions) => Promise<boolean>, ReactNode] {
  const [open, setOpen] = useState<(ConfirmOptions & { resolve: (ok: boolean) => void }) | null>(null);
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (open && !ref.current?.open) ref.current?.showModal();
  }, [open]);
  const ask = useCallback((o: ConfirmOptions) => new Promise<boolean>((resolve) => setOpen({ ...o, resolve })), []);
  const close = (ok: boolean) => {
    open?.resolve(ok);
    setOpen(null);
  };
  const dialog = open && (
    <dialog
      ref={ref}
      className="zv-confirm"
      onCancel={(e) => {
        e.preventDefault();
        close(false);
      }}
      onClick={(e) => e.target === e.currentTarget && close(false)}
    >
      <div className="zv-confirm__body">
        <p>{open.message}</p>
        <div className="zv-confirm__actions">
          <button type="button" className="zv-btn" autoFocus onClick={() => close(false)}>
            {open.cancelLabel}
          </button>
          <button type="button" className={"zv-btn " + (open.danger ? "zv-btn--danger" : "zv-btn--primary")} onClick={() => close(true)}>
            {open.confirmLabel}
          </button>
        </div>
      </div>
    </dialog>
  );
  return [ask, dialog];
}
