import { useEffect, useRef, useState } from "react";
import type { play } from "@zonventure/api";
import { Markdown, MarkdownInline, Sheet, type Translate } from "@zonventure/play-ui";

/**
 * The organisers' messages: a bell in the app bar with how many are
 * unread, a banner when one arrives, and a sheet listing them all.
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
  const [banner, setBanner] = useState<play.EventMessage | null>(null);
  const newest = messages[0]?.id ?? 0;
  const unread = messages.filter((m) => m.id > seen).length;

  // A message that arrives while the team plays gets a banner; the ones
  // waiting when the app opens only count on the bell.
  const known = useRef(newest);
  useEffect(() => {
    if (newest > known.current) {
      setBanner(messages[0] ?? null);
      // A phone in a pocket outdoors: a short buzz so "meet at 12:30"
      // isn't missed. Silently unsupported on iOS.
      try {
        navigator.vibrate?.(200);
      } catch {
        /* not allowed without a gesture on some browsers */
      }
    }
    known.current = newest;
  }, [newest, messages]);
  useEffect(() => {
    if (!banner) return;
    const id = setTimeout(() => setBanner(null), 8000);
    return () => clearTimeout(id);
  }, [banner]);

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
    setBanner(null);
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
        <button type="button" className="app-inbox__banner" onClick={show} role="status">
          <span className="app-inbox__from">{t("Message from the organisers")}</span>
          <MarkdownInline className="app-inbox__preview" text={banner.text} />
        </button>
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
