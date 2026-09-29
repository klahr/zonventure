import { useCallback, useState, type ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { DzPortal, useAnchoredPop, useDismiss } from "../../designer/dz/ui.tsx";
import { Button, ICON } from "../../ui/index.ts";

export type MenuItem =
  | { separator: true }
  | { label: string; icon?: ReactNode; href: string; download?: string; title?: string; target?: string; danger?: boolean }
  | { label: string; icon?: ReactNode; onClick: () => void; title?: string; danger?: boolean };

/** A "More ▾" button with a menu of links and actions under it. */
export function MoreMenu({ label, items, size }: { label: string; items: MenuItem[]; size?: "sm" }) {
  const [open, setOpen] = useState(false);
  const { trigger, pop, style } = useAnchoredPop<HTMLButtonElement, HTMLDivElement>(open);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, trigger, pop);
  return (
    <>
      <Button ref={trigger} {...(size ? { size } : {})} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        {label}
        <ChevronDown size={ICON.sm} aria-hidden />
      </Button>
      {open && (
        <DzPortal>
          <div
            ref={(el) => {
              pop.current = el;
              if (el && !el.contains(document.activeElement)) el.querySelector<HTMLElement>("a,button")?.focus();
            }}
            className="ev-menu"
            role="menu"
            style={style}
            onKeyDown={(e) => {
              if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
              e.preventDefault();
              const all = [...(pop.current?.querySelectorAll<HTMLElement>("a,button") ?? [])];
              const i = all.indexOf(document.activeElement as HTMLElement);
              all[(i + (e.key === "ArrowDown" ? 1 : all.length - 1)) % all.length]?.focus();
            }}
          >
            {items.map((it, i) => {
              if ("separator" in it) return <div key={i} className="ev-menu__sep" role="separator" />;
              const cls = "ev-menu__item" + (it.danger ? " is-danger" : "");
              if ("href" in it) {
                return (
                  <a key={i} className={cls} role="menuitem" href={it.href} title={it.title} onClick={close}
                    {...(it.download ? { download: it.download } : {})}
                    {...(it.target ? { target: it.target, rel: "noopener" } : {})}>
                    {it.icon}
                    {it.label}
                  </a>
                );
              }
              return (
                <button key={i} type="button" className={cls} role="menuitem" title={it.title} onClick={() => { close(); it.onClick(); }}>
                  {it.icon}
                  {it.label}
                </button>
              );
            })}
          </div>
        </DzPortal>
      )}
    </>
  );
}
