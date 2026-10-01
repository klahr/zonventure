import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { AlertCircle, CheckCircle2, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { errorText } from "./load.ts";

interface Toast {
  id: number;
  tone: "ok" | "err";
  text: string;
}
interface ToastApi {
  ok: (text: string) => void;
  /** Show an error; an Error or ApiError shows its message. */
  err: (e: unknown) => void;
}

const Ctx = createContext<ToastApi>({ ok: () => {}, err: () => {} });

/** How long a toast stays: errors twice as long, as they need reading. */
const TOAST_MS = { ok: 3500, err: 7000 } as const;
/** The exit animation's length (console.css). */
const LEAVE_MS = 180;

/**
 * Short confirmations ("Saved") and failures, bottom centre, as cards:
 * an icon and an edge in the tone's colour on the ordinary surface, so a
 * failure reads clearly without shouting. Pointing at one holds it, so
 * a long message can be read; × dismisses it. Errors are alerts to
 * screen readers, confirmations polite.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Toast[]>([]);
  const drop = useCallback((id: number) => setList((l) => l.filter((t) => t.id !== id)), []);
  const push = useCallback((tone: Toast["tone"], text: string) => {
    const id = Date.now() + Math.random();
    setList((l) => [...l.slice(-2), { id, tone, text }]);
  }, []);
  const api: ToastApi = { ok: (t) => push("ok", t), err: (e) => push("err", typeof e === "string" ? e : errorText(e)) };
  return (
    <Ctx.Provider value={api}>
      {children}
      {createPortal(
        <div className="dz cs-toasts">
          {list.map((t) => (
            <ToastCard key={t.id} toast={t} onGone={() => drop(t.id)} />
          ))}
        </div>,
        document.body,
      )}
    </Ctx.Provider>
  );
}

function ToastCard({ toast, onGone }: { toast: Toast; onGone: () => void }) {
  const t = useT();
  const [leaving, setLeaving] = useState(false);
  const [held, setHeld] = useState(false);
  const gone = useRef(onGone);
  gone.current = onGone;
  useEffect(() => {
    if (held || leaving) return;
    const id = setTimeout(() => setLeaving(true), TOAST_MS[toast.tone]);
    return () => clearTimeout(id);
  }, [held, leaving, toast.tone]);
  useEffect(() => {
    if (!leaving) return;
    const id = setTimeout(() => gone.current(), LEAVE_MS);
    return () => clearTimeout(id);
  }, [leaving]);
  const Icon = toast.tone === "err" ? AlertCircle : CheckCircle2;
  return (
    <div
      className={`cs-toast cs-toast--${toast.tone}` + (leaving ? " is-leaving" : "")}
      role={toast.tone === "err" ? "alert" : "status"}
      onPointerEnter={() => setHeld(true)}
      onPointerLeave={() => setHeld(false)}
    >
      <Icon className="cs-toast__icon" size={18} aria-hidden />
      <span className="cs-toast__text">{toast.text}</span>
      <button type="button" className="cs-toast__close" onClick={() => setLeaving(true)} aria-label={t.t("Dismiss")}>
        <X size={16} aria-hidden />
      </button>
    </div>
  );
}

export const useToast = () => useContext(Ctx);
