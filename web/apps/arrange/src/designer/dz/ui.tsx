// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dazyflow's UI primitives (web/src/components/ui/*), ported so the
// designer behaves like dazyflow's editor. Changes from the originals:
// react-i18next is replaced by this app's useT, and every portal renders
// inside a .dz wrapper so the scoped styles in ../dz.css apply to it.

import {
  forwardRef,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type CSSProperties,
  type FormEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, CheckCircle2, Info, X, XCircle, type LucideIcon } from "lucide-react";
import { useT } from "../../i18n.tsx";

export const ICON = { xs: 12, sm: 14, md: 16, lg: 18, xl: 20 } as const;

/** Portal to <body>, inside .dz so the scoped styles reach it. */
export function DzPortal({ children }: { children: ReactNode }) {
  return createPortal(<div className="dz">{children}</div>, document.body);
}

// --- Button ----------------------------------------------------------------

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger" | "warning" | "link";
type ButtonSize = "md" | "sm" | "icon";

type ButtonProps = {
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: ReactNode;
  block?: boolean;
  loading?: boolean;
  filled?: boolean;
  className?: string;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "className">;

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "secondary", size = "md", icon, block, loading, filled, className, children, disabled, type, ...rest },
  ref,
) {
  const parts: string[] = [];
  if (variant !== "secondary") parts.push(variant);
  if (size !== "md") parts.push(size);
  if (block) parts.push("btn-block");
  if (filled) parts.push("filled");
  if (className) parts.push(className);
  return (
    <button
      ref={ref}
      type={type ?? "button"}
      className={parts.join(" ") || undefined}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {icon}
      {children}
    </button>
  );
});

// --- small pieces ------------------------------------------------------------

const CALLOUT: Record<"warning" | "info" | "danger" | "success", LucideIcon> = {
  warning: AlertTriangle,
  info: Info,
  danger: XCircle,
  success: CheckCircle2,
};

export function Callout({ variant = "info", children }: { variant?: keyof typeof CALLOUT; children: ReactNode }) {
  const Icon = CALLOUT[variant];
  return (
    <div className={`callout callout-${variant}`} role="note">
      <Icon size={ICON.md} className="callout-icon" aria-hidden="true" />
      <div className="callout-body">{children}</div>
    </div>
  );
}

export function Notice({ children }: { children: ReactNode }) {
  return <div className="notice-line">{children}</div>;
}

// --- hooks -----------------------------------------------------------------

// Open dialogs, newest last. Escape closes only the newest, so a confirm
// opened over a dialog doesn't take the dialog with it.
const escapeStack: { current: () => void }[] = [];

export function useEscapeToClose(onClose: () => void) {
  const ref = useRef(onClose);
  ref.current = onClose;
  useEffect(() => {
    const entry = ref;
    escapeStack.push(entry);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && escapeStack[escapeStack.length - 1] === entry) entry.current();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      const i = escapeStack.lastIndexOf(entry);
      if (i >= 0) escapeStack.splice(i, 1);
    };
  }, []);
}

type AnchorPos = { left: number; top: number };

// anchorBelow places a panel under its trigger, flipping above when there
// is no room below and clamping to the viewport (dazyflow lib/anchorPop).
function anchorBelow(trigger: DOMRect, size: { width: number; height: number }, align: "left" | "right" = "right"): AnchorPos {
  const gap = 6;
  const margin = 8;
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  let top = trigger.bottom + gap;
  const above = trigger.top - gap - size.height;
  if (top + size.height > vh - margin && above >= margin) top = above;
  let left = align === "right" ? trigger.right - size.width : trigger.left;
  left = Math.max(margin, Math.min(left, vw - margin - size.width));
  top = Math.max(margin, Math.min(top, vh - margin - size.height));
  return { left, top };
}

export function useAnchoredPop<T extends HTMLElement, P extends HTMLElement>(open: boolean, align: "left" | "right" = "right") {
  const trigger = useRef<T | null>(null);
  const pop = useRef<P | null>(null);
  const [pos, setPos] = useState<AnchorPos | null>(null);
  const place = useCallback(() => {
    const t = trigger.current?.getBoundingClientRect();
    const p = pop.current?.getBoundingClientRect();
    if (t && p) setPos(anchorBelow(t, { width: p.width, height: p.height }, align));
  }, [align]);
  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open, place]);
  const style: CSSProperties = {
    position: "fixed",
    left: pos?.left ?? 0,
    top: pos?.top ?? 0,
    visibility: pos ? "visible" : "hidden",
  };
  return { trigger, pop, style };
}

/** Close an anchored panel on Escape or a press outside it and its trigger. */
export function useDismiss(open: boolean, close: () => void, trigger: React.RefObject<HTMLElement | null>, pop: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      close();
      trigger.current?.focus();
    };
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node;
      if (trigger.current?.contains(target) || pop.current?.contains(target)) return;
      close();
    };
    document.addEventListener("keydown", onKey, true);
    document.addEventListener("pointerdown", onDown, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      document.removeEventListener("pointerdown", onDown, true);
    };
  }, [open, close, trigger, pop]);
}

// --- HelpPopover -------------------------------------------------------------

export function HelpPopover({ label, body }: { label: string; body: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const { trigger, pop, style } = useAnchoredPop<HTMLButtonElement, HTMLSpanElement>(open);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, trigger, pop);
  return (
    <span className="help-pop-wrap">
      <button
        ref={trigger}
        type="button"
        className="inspector-info"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        title={label}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
      >
        <Info size={ICON.sm} aria-hidden="true" />
      </button>
      {open && (
        <DzPortal>
          <span className="help-pop" id={id} role="note" ref={pop} style={style}>
            {body}
          </span>
        </DzPortal>
      )}
    </span>
  );
}

// --- IssuesButton (dazyflow components/editor/IssuesPopover.tsx) -------------

export function IssuesButton({
  kind,
  count,
  title,
  heading,
  open,
  onOpenChange,
  children,
}: {
  kind: "error" | "warning";
  count: number;
  title: string;
  heading: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  const id = useId();
  const { trigger, pop, style } = useAnchoredPop<HTMLButtonElement, HTMLDivElement>(open);
  const close = useCallback(() => onOpenChange(false), [onOpenChange]);
  useDismiss(open, close, trigger, pop);
  if (count <= 0) return null;
  const Icon = kind === "error" ? XCircle : AlertTriangle;
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="editor-issues-btn"
        data-kind={kind}
        title={title}
        aria-label={title}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={(e) => {
          e.stopPropagation();
          onOpenChange(!open);
        }}
      >
        <Icon size={ICON.sm} aria-hidden="true" />
        <span className="editor-issues-count">{count}</span>
      </button>
      {open && (
        <DzPortal>
          <div id={id} ref={pop} style={style} className="editor-issues-pop" data-kind={kind} role="dialog" aria-label={heading}>
            <div className="editor-issues-pop-head">{heading}</div>
            <div className="editor-issues-pop-body">{children}</div>
          </div>
        </DzPortal>
      )}
    </>
  );
}

export function IssueRow({ text, actions }: { text: ReactNode; actions?: ReactNode }) {
  return (
    <div className="editor-issues-row">
      <div className="editor-issues-row-text">{text}</div>
      {actions && <div className="editor-issues-row-actions">{actions}</div>}
    </div>
  );
}

// --- modals ----------------------------------------------------------------

export type ModalSize = "sm" | "md" | "lg" | "xl";

/**
 * Every modal in the console and the designer: one shell, so they all
 * look and behave the same. A head with the title (an icon and a line
 * under it, optionally) and a close button, a body that scrolls, and a
 * footer of the dialog's own buttons. Escape or a click outside closes
 * it. Focus goes in — to a field marked autoFocus, else the first field,
 * else the dialog — and back to where it was on close. With onSubmit the
 * body is a form, so Enter submits.
 *
 * Sizes: sm for a question, md (the default) for a form, lg for a list
 * or a preview, xl for a workspace. A flush body has no padding and does
 * not scroll, for a dialog that lays out its own panes.
 */
export function Modal(props: {
  title: ReactNode;
  icon?: ReactNode;
  subtitle?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  onSubmit?: () => void;
  size?: ModalSize;
  /** A picture across the top, edge to edge, above the title. */
  banner?: ReactNode;
  flush?: boolean;
  /** An alertdialog asks before something; the body is then its message. */
  alert?: boolean;
  className?: string;
}) {
  const t = useT();
  const id = useId();
  useEscapeToClose(props.onClose);
  const box = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const el = box.current;
    if (el && !el.contains(document.activeElement)) {
      const first = el.querySelector<HTMLElement>(".modal-body [autofocus], .modal-body input:not([type=hidden]):not([disabled]), .modal-body select, .modal-body textarea");
      (first ?? el).focus();
    }
    return () => before?.focus?.();
  }, []);
  const Body = props.onSubmit ? "form" : "div";
  const cls = ["modal", `modal--${props.size ?? "md"}`, props.flush && "modal--flush", props.className].filter(Boolean).join(" ");
  return (
    <DzPortal>
      {/* Mouse down, not click: a text selection dragged out of the dialog doesn't close it. */}
      <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && props.onClose()}>
        <Body
          ref={(el: HTMLElement | null) => {
            box.current = el;
          }}
          tabIndex={-1}
          className={cls}
          role={props.alert ? "alertdialog" : "dialog"}
          aria-modal="true"
          aria-labelledby={id}
          {...(props.onSubmit
            ? {
                onSubmit: (e: FormEvent) => {
                  e.preventDefault();
                  props.onSubmit!();
                },
              }
            : {})}
        >
          {props.banner && <div className="modal-banner">{props.banner}</div>}
          <div className="modal-head">
            {props.icon && (
              <span className="modal-head__icon" aria-hidden="true">
                {props.icon}
              </span>
            )}
            <div className="modal-head__text">
              <h2 id={id}>{props.title}</h2>
              {props.subtitle && <p className="modal-head__sub">{props.subtitle}</p>}
            </div>
            <Button variant="ghost" size="icon" className="modal-close" onClick={props.onClose} aria-label={t.t("Close")} title={t.t("Close")}>
              <X size={ICON.md} />
            </Button>
          </div>
          <div className="modal-body">{props.children}</div>
          {props.footer && <div className="modal-foot">{props.footer}</div>}
        </Body>
      </div>
    </DzPortal>
  );
}

export function PromptModal({
  title,
  label,
  hint,
  initialValue,
  confirmLabel,
  allowEmpty,
  onSubmit,
  onCancel,
}: {
  title: string;
  label: string;
  hint?: string;
  initialValue?: string;
  confirmLabel: string;
  allowEmpty?: boolean;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState(initialValue ?? "");
  const input = useRef<HTMLInputElement>(null);
  const fieldId = useId();
  useEffect(() => input.current?.select(), []);
  return (
    <Modal
      title={title}
      size="sm"
      onClose={onCancel}
      onSubmit={() => {
        if (allowEmpty || value.trim()) onSubmit(value.trim());
      }}
      footer={
        <>
          <Button onClick={onCancel}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" disabled={!allowEmpty && !value.trim()}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className="sf-field">
        <div className="label-row">
          <label htmlFor={fieldId}>{label}</label>
        </div>
        <input id={fieldId} ref={input} value={value} onChange={(e) => setValue(e.target.value)} spellCheck={false} autoComplete="off" />
        {hint && <p className="desc">{hint}</p>}
      </div>
    </Modal>
  );
}

/**
 * Asks before an action, a delete most often. Cancel has the focus, so
 * Enter or Escape on a dialog opened by mistake keeps things as they were.
 */
export function ConfirmModal({
  title,
  message,
  confirmLabel,
  cancelLabel,
  danger,
  onConfirm,
  onCancel,
}: {
  title: string;
  message?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => cancel.current?.focus(), []);
  return (
    <Modal
      title={title}
      size="sm"
      alert
      onClose={onCancel}
      footer={
        <>
          <Button ref={cancel} onClick={onCancel}>
            {cancelLabel ?? t.t("Cancel")}
          </Button>
          <Button variant={danger ? "danger" : "primary"} filled={!!danger} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {message && <p className="confirm-message">{message}</p>}
    </Modal>
  );
}
