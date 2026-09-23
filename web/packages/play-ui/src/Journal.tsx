import { useMemo, useState } from "react";
import type { play } from "@zonventure/api";
import type { Translate } from "./feedback.ts";
import { Markdown } from "./Markdown.tsx";
import { MediaList } from "./Media.tsx";

/**
 * Which notes a team has already opened the journal on, kept on the phone
 * per team. It only decides what is marked "new" — the notes themselves
 * always come from the server.
 */
function seenKey(storageKey: string) {
  return `zv.journal:${storageKey}`;
}
function readSeen(storageKey: string): Set<string> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(seenKey(storageKey)) ?? "[]");
    return new Set(Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);
  } catch {
    return new Set();
  }
}
function writeSeen(storageKey: string, slugs: string[]) {
  try {
    localStorage.setItem(seenKey(storageKey), JSON.stringify(slugs));
  } catch {
    /* private mode: every note stays "new" until reload */
  }
}

/** How many notes the team has not opened the journal on yet. */
export function useUnreadNotes(journal: play.JournalView[] | undefined, storageKey: string, open: boolean): number {
  // Recomputed when the journal changes or the sheet closes.
  return useMemo(() => {
    const seen = readSeen(storageKey);
    return (journal ?? []).filter((j) => !seen.has(j.slug)).length;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [journal, storageKey, open]);
}

/**
 * The journal: every note the team has found, grouped under its section.
 * Notes new since the journal was last opened are marked; opening it
 * marks everything read.
 */
export function JournalNotes({ journal, storageKey, t }: { journal: play.JournalView[]; storageKey: string; t: Translate }) {
  // Taken once, when the sheet opens, so "new" does not vanish mid-read.
  const [fresh] = useState(() => {
    const seen = readSeen(storageKey);
    const out = new Set(journal.filter((j) => !seen.has(j.slug)).map((j) => j.slug));
    writeSeen(storageKey, journal.map((j) => j.slug));
    return out;
  });

  if (journal.length === 0) return <p className="pv-muted">{t("Nothing noted yet. What you find along the way is written down here.")}</p>;

  // Consecutive notes of one section share its heading.
  const groups: { section: string; notes: play.JournalView[] }[] = [];
  for (const j of journal) {
    const last = groups[groups.length - 1];
    if (last && last.section === (j.section ?? "")) last.notes.push(j);
    else groups.push({ section: j.section ?? "", notes: [j] });
  }

  return (
    <div className="pv-journal-list">
      {groups.map((g, i) => (
        <section key={i} className="pv-journal-group">
          {g.section && <h3 className="pv-journal-group__title">{g.section}</h3>}
          {g.notes.map((j) => (
            <article key={j.slug} className={"pv-journal" + (fresh.has(j.slug) ? " is-new" : "")}>
              {j.title && (
                <h4>
                  {j.title}
                  {fresh.has(j.slug) && <span className="pv-journal__new">{t("New")}</span>}
                </h4>
              )}
              <MediaList media={j.media} />
              {j.body && <Markdown text={j.body} />}
            </article>
          ))}
        </section>
      ))}
    </div>
  );
}
