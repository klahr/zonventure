import { useEffect, useRef, useState } from "react";
import type { play } from "@zonventure/api";
import { Markdown, MarkdownInline, Sheet, type Translate } from "@zonventure/play-ui";

/** How long a new message's banner shows in full before it folds to one line. */
const FOLD_MS = 8000;

/**
 * The organisers' messages: a bell in the app bar with how many are
 * unread, a banner while one is unread, and a sheet listing them all.
 * Information beside the game — none of it moves the team.
 *
 * Read is remembered per team on this phone, as the newest id seen.
 */
export function Inbox({ messages, teamID, t, locale }: { messages: play.EventMessage[]; teamID: number; t: Translate; locale: string }) {
  const key = `zv-inbox-seen-${teamID}`;
  const [seen, setSeen] = useState(() => {
    try {
      return Number(localStorage.getItem(key)) || 0;
    } catch {
      return 0;
    }
  });
  const [open, setOpen] = useState(false);
  const newest = messages[0]?.id ?? 0;
  const unread = messages.filter((m) => m.id > seen).length;
  // The newest unread message stays on a banner until it is read or
  // waved away: a glance at the screen in the street must catch it.
  const [dismissed, setDismissed] = useState(0);
  const banner = unread > 0 && newest > dismissed ? messages[0] : null;
  // Once seen, the banner folds to one line, and stays folded on this
  // phone: the message is at hand without covering every step. A tap
  // unfolds it.
  const foldKey = `zv-inbox-folded-${teamID}`;
  const [folded, setFolded] = useState(() => {
    try {
      return Number(localStorage.getItem(foldKey)) || 0;
    } catch {
      return 0;
    }
  });
  const [unfolded, setUnfolded] = useState(0);
  const bannerID = banner?.id ?? 0;
  useEffect(() => {
    if (!bannerID || bannerID <= folded) return;
    const id = setTimeout(() => {
      setFolded(bannerID);
      try {
        localStorage.setItem(foldKey, String(bannerID));
      } catch {
        /* private mode: it shows in full again next time */
      }
    }, FOLD_MS);
    return () => clearTimeout(id);
  }, [bannerID, folded, foldKey]);
  const compact = !!banner && banner.id <= folded && unfolded !== banner.id;

  // A message that arrives while the team plays also buzzes.
  const known = useRef(newest);
  useEffect(() => {
    if (newest > known.current) {
      // A phone in a pocket outdoors: a short buzz so "meet at 12:30"
      // isn't missed. Silently unsupported on iOS.
      try {
        navigator.vibrate?.(200);
      } catch {
        /* not allowed without a gesture on some browsers */
      }
    }
    known.current = newest;
  }, [newest]);

  const markRead = () => {
    setSeen(newest);
    try {
      localStorage.setItem(key, String(newest));
    } catch {
      /* private mode: read again next time */
    }
  };
  const show = () => {
    setOpen(true);
    markRead();
  };
  // 24-hour like the organisers' screens; older than today gets the date.
  const when = (iso: string) => {
    const d = new Date(iso);
    const today = d.toDateString() === new Date().toDateString();
    return d.toLocaleString(locale, { ...(today ? {} : { day: "numeric", month: "short" }), hour: "2-digit", minute: "2-digit", hour12: false });
  };

  return (
    <>
      <button type="button" className="app-inbox" onClick={show} aria-label={unread ? t("Messages, {{n}} unread", { n: unread }) : t("Messages")}>
        <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
          <path d="M12 3a6 6 0 0 0-6 6v3.6L4.3 16a1 1 0 0 0 .9 1.5h13.6a1 1 0 0 0 .9-1.5L18 12.6V9a6 6 0 0 0-6-6Zm-2.2 16a2.3 2.3 0 0 0 4.4 0Z" fill="currentColor" />
        </svg>
        {unread > 0 && <span className="app-inbox__count">{unread}</span>}
      </button>
      {banner && (
        <div className={"app-inbox__banner" + (compact ? " app-inbox__banner--folded" : "")} role="status">
          <button type="button" className="app-inbox__open" onClick={compact ? () => setUnfolded(banner.id) : show} {...(compact ? { "aria-expanded": false } : {})}>
            <span className="app-inbox__from">{t("Message from the organisers")}</span>
            <MarkdownInline className="app-inbox__preview" text={banner.text} />
          </button>
          <button type="button" className="app-inbox__close" onClick={() => setDismissed(newest)} aria-label={t("Hide")}>
            ×
          </button>
        </div>
      )}
      {open && (
        <div className="pv app-inbox__pv">
          <Sheet title={t("Messages")} t={t} onClose={() => setOpen(false)}>
            <div className="pv-sheet__body app-inbox__list">
              {messages.length === 0 ? (
                <p className="app-inbox__none">{t("No messages from the organisers yet.")}</p>
              ) : (
                messages.map((m) => (
                  <article key={m.id} className="app-inbox__msg">
                    <time dateTime={m.created_at}>{when(m.created_at)}</time>
                    <Markdown text={m.text} />
                  </article>
                ))
              )}
            </div>
          </Sheet>
        </div>
      )}
    </>
  );
}
