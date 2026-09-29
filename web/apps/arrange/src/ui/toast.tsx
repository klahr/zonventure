import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
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

/**
 * Short confirmations ("Saved") and failures, bottom centre, announced
 * to screen readers. Errors stay twice as long: they need reading.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Toast[]>([]);
  const push = useCallback((tone: Toast["tone"], text: string) => {
    const id = Date.now() + Math.random();
    setList((l) => [...l.slice(-2), { id, tone, text }]);
    setTimeout(() => setList((l) => l.filter((t) => t.id !== id)), tone === "err" ? 7000 : 3500);
  }, []);
  const api: ToastApi = { ok: (t) => push("ok", t), err: (e) => push("err", typeof e === "string" ? e : errorText(e)) };
  return (
    <Ctx.Provider value={api}>
      {children}
      {createPortal(
        <div className="dz cs-toasts" role="status" aria-live="polite">
          {list.map((t) => (
            <div key={t.id} className={`cs-toast cs-toast--${t.tone}`}>{t.text}</div>
          ))}
        </div>,
        document.body,
      )}
    </Ctx.Provider>
  );
}

export const useToast = () => useContext(Ctx);
