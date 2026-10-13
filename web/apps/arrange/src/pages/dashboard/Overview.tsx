import type { ReactNode } from "react";
import { Pause, Terminal, Users } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { relTime, toDate } from "../../ui/index.ts";
import type { EventRow } from "../events/api.ts";
import { StartNowButton, eventLabel } from "../events/parts.tsx";

type T = ReturnType<typeof useT>;

/** A headline number with its icon and one line of context. */
export function Tile({ icon, value, label, hint, href: to, tone }: { icon: ReactNode; value: number; label: string; hint?: ReactNode; href?: string; tone?: "live" }) {
  const body = (
    <>
      <span className="db-tile__icon" aria-hidden>{icon}</span>
      <span className="db-tile__text">
        <span className="db-tile__value">{value}</span>
        <span className="db-tile__label">{label}</span>
        {hint && <span className="db-tile__hint">{hint}</span>}
      </span>
    </>
  );
  const cls = "db-tile" + (tone ? ` db-tile--${tone}` : "") + (to ? " db-tile--link" : "");
  return to ? <a className={cls} href={to}>{body}</a> : <div className={cls}>{body}</div>;
}

function counts(t: T, r: EventRow) {
  return (
    <span className="db-meta">
      <Users size={14} aria-hidden />
      {t.plural(r.teams_count, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") })}
      <span aria-hidden>·</span>
      {t.plural(r.player_slots_count, { one: t.t("{{n}} player"), other: t.t("{{n}} players") })}
    </span>
  );
}

/** A running or paused event: what it is, how long it has run, the way into its controls. */
export function LiveCard({ event: r }: { event: EventRow }) {
  const t = useT();
  const loc = t.current();
  const paused = r.state === "paused";
  return (
    <article className={"db-live" + (paused ? " is-paused" : "")}>
      <div className="db-live__top">
        <span className="db-live__state">
          {paused ? <Pause size={12} aria-hidden /> : <span className="db-live__dot" aria-hidden />}
          {paused ? t.t("Paused") : t.t("Live")}
        </span>
        {r.started_at && <span className="db-live__since" title={r.started_at}>{t.t("Started {{when}}", { when: relTime(r.started_at, loc) })}</span>}
      </div>
      <a className="db-live__name db-stretch" href={href({ name: "event", id: r.id })}>{eventLabel(r)}</a>
      <span className="db-live__adv">
        {r.adventure_avatar_url && <img src={r.adventure_avatar_url} alt="" />}
        {r.adventure_title || r.adventure_slug}
      </span>
      <div className="db-live__foot">
        {counts(t, r)}
        <a className="btn sm db-live__ctl" href={href({ name: "event-control", id: r.id })}>
          <Terminal size={14} aria-hidden />
          {t.t("Live control")}
        </a>
      </div>
    </article>
  );
}

/** The next scheduled events as a short agenda: a date tile, the event, its start. */
export function Agenda({ events, onStarted }: { events: EventRow[]; onStarted: () => void }) {
  const t = useT();
  const loc = t.current();
  return (
    <ol className="db-agenda">
      {events.map((r) => {
        const d = toDate(r.scheduled_at);
        return (
          <li key={r.id} className="db-agenda__item">
            <span className={"db-date" + (d ? "" : " is-none")} aria-hidden>
              {d ? (
                <>
                  <span className="db-date__mon">{d.toLocaleDateString(loc, { month: "short" })}</span>
                  <span className="db-date__day">{d.getDate()}</span>
                </>
              ) : (
                "—"
              )}
            </span>
            <span className="db-agenda__text">
              <a className="db-agenda__name db-stretch" href={href({ name: "event", id: r.id })}>{eventLabel(r)}</a>
              <span className="db-agenda__sub">
                {d ? (
                  <>
                    <time dateTime={r.scheduled_at}>{d.toLocaleString(loc, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</time>
                    <span className="db-agenda__rel">{relTime(r.scheduled_at, loc)}</span>
                  </>
                ) : (
                  <span>{r.party ? t.t("Starts when the first team joins") : t.t("Starts when you start it")}</span>
                )}
              </span>
              <span className="db-agenda__sub">
                {r.adventure_title || r.adventure_slug}
                <span aria-hidden>·</span>
                {t.plural(r.teams_count, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") })}
              </span>
            </span>
            <span className="db-agenda__go">
              <StartNowButton event={r} onStarted={onStarted} />
            </span>
          </li>
        );
      })}
    </ol>
  );
}
