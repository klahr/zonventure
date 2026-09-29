import { useId, type ReactNode } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

/**
 * The console's page-level building blocks. Buttons, inputs, dialogs and
 * callouts are the designer's own (../designer/dz/ui.tsx, styled by
 * dz.css under the .dz wrapper the shell puts round every page); these
 * add what a page of the console needs on top, drawn in the same tokens
 * (console.css), so the dashboard and the designer read as one app.
 *
 * Every screen composes these rather than its own markup, so a change
 * of look is made here once.
 */

/** A page: header, then its sections. */
export function Page({ children, wide }: { children: ReactNode; wide?: boolean }) {
  return <div className={"cs-page" + (wide ? " cs-page--wide" : "")}>{children}</div>;
}

/**
 * The page's title row: an optional back link above, the title and a
 * line under it on the left, the page's actions on the right.
 */
export function PageHeader(props: {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  back?: { href: string; label: string };
  badges?: ReactNode;
}) {
  return (
    <header className="cs-head">
      {props.back && (
        <a className="cs-back" href={props.back.href}>
          <ChevronLeft size={16} aria-hidden />
          {props.back.label}
        </a>
      )}
      <div className="cs-head__row">
        <div className="cs-head__text">
          <h1 className="cs-title">
            {props.title}
            {props.badges && <span className="cs-head__badges">{props.badges}</span>}
          </h1>
          {props.subtitle && <p className="cs-subtitle">{props.subtitle}</p>}
        </div>
        {props.actions && <div className="cs-head__actions">{props.actions}</div>}
      </div>
    </header>
  );
}

/** A section of a page: a titled panel with optional actions. */
export function Card(props: { title?: ReactNode; subtitle?: ReactNode; actions?: ReactNode; children: ReactNode; tone?: "danger"; flush?: boolean; id?: string }) {
  const id = useId();
  return (
    <section
      className={"cs-card" + (props.tone ? ` cs-card--${props.tone}` : "") + (props.flush ? " cs-card--flush" : "")}
      aria-labelledby={props.title ? id : undefined}
      id={props.id}
    >
      {(props.title || props.actions) && (
        <div className="cs-card__head">
          <div>
            {props.title && <h2 id={id} className="cs-card__title">{props.title}</h2>}
            {props.subtitle && <p className="cs-card__sub">{props.subtitle}</p>}
          </div>
          {props.actions && <div className="cs-card__actions">{props.actions}</div>}
        </div>
      )}
      <div className="cs-card__body">{props.children}</div>
    </section>
  );
}

/** A row of big numbers; the grid fits however many there are. */
export function Stats({ children }: { children: ReactNode }) {
  return <div className="cs-stats">{children}</div>;
}

export function Stat({ label, value, href, tone }: { label: ReactNode; value: ReactNode; href?: string; tone?: "good" | "warn" }) {
  const body = (
    <>
      <span className={"cs-stat__value" + (tone ? ` is-${tone}` : "")}>{value}</span>
      <span className="cs-stat__label">{label}</span>
    </>
  );
  return href ? <a className="cs-stat cs-stat--link" href={href}>{body}</a> : <div className="cs-stat">{body}</div>;
}

export interface Column<T> {
  key: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  /** Hidden on narrow screens. */
  wide?: boolean;
  align?: "end";
  width?: string;
}

/**
 * A data table. A row with an href is a link as a whole (the first cell
 * holds the real anchor, so it is keyboard- and screen-reader-friendly),
 * and the table scrolls sideways inside its card on a phone.
 */
export function Table<T>(props: { columns: Column<T>[]; rows: T[]; rowKey: (r: T) => string | number; rowHref?: (r: T) => string | undefined; empty?: ReactNode; caption?: string }) {
  if (props.rows.length === 0 && props.empty) return <>{props.empty}</>;
  return (
    <div className="cs-table-wrap">
      <table className="cs-table">
        {props.caption && <caption className="cs-sr">{props.caption}</caption>}
        <thead>
          <tr>
            {props.columns.map((c) => (
              <th key={c.key} className={(c.wide ? "cs-wide " : "") + (c.align === "end" ? "cs-end" : "")} style={c.width ? { width: c.width } : undefined}>
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {props.rows.map((r) => {
            const link = props.rowHref?.(r);
            return (
              <tr key={props.rowKey(r)} className={link ? "is-link" : undefined} onClick={link ? (e) => { if (!(e.target as HTMLElement).closest("a,button,input,select,label")) location.href = link; } : undefined}>
                {props.columns.map((c, i) => (
                  <td key={c.key} className={(c.wide ? "cs-wide " : "") + (c.align === "end" ? "cs-end" : "")}>
                    {i === 0 && link ? <a href={link} className="cs-rowlink">{c.cell(r)}</a> : c.cell(r)}
                  </td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** Tabs that are links, so each tab has a URL (#/settings/billing). */
export function Tabs({ tabs, current, label }: { tabs: { id: string; label: ReactNode; href: string; hidden?: boolean }[]; current: string; label: string }) {
  return (
    <nav className="cs-tabs" aria-label={label}>
      {tabs.filter((t) => !t.hidden).map((t) => (
        <a key={t.id} href={t.href} className={"cs-tab" + (t.id === current ? " is-on" : "")} {...(t.id === current ? { "aria-current": "page" as const } : {})}>
          {t.label}
        </a>
      ))}
    </nav>
  );
}

export type Tone = "neutral" | "good" | "warn" | "bad" | "info" | "accent";

/** A small status label. */
export function Pill({ tone = "neutral", children, title }: { tone?: Tone; children: ReactNode; title?: string }) {
  return <span className={`cs-pill cs-pill--${tone}`} title={title}>{children}</span>;
}

/** What a list or section shows when there is nothing in it yet, with the way to change that. */
export function Empty({ title, children, action }: { title: ReactNode; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="cs-empty">
      <p className="cs-empty__title">{title}</p>
      {children && <p className="cs-empty__text">{children}</p>}
      {action && <div className="cs-empty__action">{action}</div>}
    </div>
  );
}

/** A labelled form control, with its hint or its error underneath. */
export function Field({ label, hint, error, children, inline }: { label: ReactNode; hint?: ReactNode; error?: ReactNode; children: ReactNode; inline?: boolean }) {
  return (
    <label className={"cs-field" + (inline ? " cs-field--inline" : "")}>
      <span className="cs-field__label">{label}</span>
      {children}
      {error ? <span className="cs-field__error" role="alert">{error}</span> : hint ? <span className="cs-field__hint">{hint}</span> : null}
    </label>
  );
}

/** Controls side by side, wrapping on a phone. */
export function Row({ children, end, gap }: { children: ReactNode; end?: boolean; gap?: "sm" }) {
  return <div className={"cs-row" + (end ? " cs-row--end" : "") + (gap ? ` cs-row--${gap}` : "")}>{children}</div>;
}

/** Name/value pairs, such as an event's facts. */
export function Facts({ items }: { items: { label: ReactNode; value: ReactNode }[] }) {
  return (
    <dl className="cs-facts">
      {items.map((it, i) => (
        <div key={i} className="cs-facts__item">
          <dt>{it.label}</dt>
          <dd>{it.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** A clickable card in a grid, such as an adventure. */
export function Tile(props: { href: string; title: ReactNode; meta?: ReactNode; children?: ReactNode; media?: ReactNode; badge?: ReactNode }) {
  return (
    <a className="cs-tile" href={props.href}>
      {props.media && <span className="cs-tile__media">{props.media}</span>}
      <span className="cs-tile__text">
        <span className="cs-tile__title">{props.title}{props.badge && <span className="cs-tile__badge">{props.badge}</span>}</span>
        {props.meta && <span className="cs-tile__meta">{props.meta}</span>}
        {props.children && <span className="cs-tile__body">{props.children}</span>}
      </span>
      <ChevronRight size={16} className="cs-tile__go" aria-hidden />
    </a>
  );
}

export function Tiles({ children }: { children: ReactNode }) {
  return <div className="cs-tiles">{children}</div>;
}

/** A name's initials on a coloured square, or the uploaded picture. */
export function Avatar({ name, src, size = 32, color }: { name: string; src?: string | undefined; size?: number; color?: string }) {
  const style = { width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.42)), ...(color ? { background: color, color: "#fff" } : {}) };
  if (src) return <img className="cs-avatar" src={src} alt="" style={style} />;
  return <span className="cs-avatar" style={style} aria-hidden>{initials(name)}</span>;
}

/** Up to two letters from a name, ignoring punctuation ("Bildpussel (test)" → "BT"). */
export function initials(name: string): string {
  const parts = (name || "").trim().split(/\s+/).map((p) => p.replace(/[^\p{L}\p{N}]/gu, "")).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0]!.slice(0, 2).toUpperCase();
  return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
}

/** The loading placeholder for a page or a card. */
export function Loading({ label }: { label: string }) {
  return <p className="cs-loading" aria-busy="true">{label}</p>;
}

/** An error in place of content, with a retry when there is one. */
export function ErrorBox({ message, retry, retryLabel }: { message: string; retry?: () => void; retryLabel?: string }) {
  return (
    <div className="cs-error" role="alert">
      <span>{message}</span>
      {retry && <button type="button" className="sm" onClick={retry}>{retryLabel}</button>}
    </div>
  );
}

/** Visually hidden, still read out. */
export function SrOnly({ children }: { children: ReactNode }) {
  return <span className="cs-sr">{children}</span>;
}
