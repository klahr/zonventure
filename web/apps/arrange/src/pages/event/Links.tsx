import { useState } from "react";
import { Copy, ExternalLink } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, fmtDateTime, ICON, toDate, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventInfo, Team } from "./api.ts";
import { copyText, downloadBlob, liveURL, memberURL, members, teamURL } from "./util.ts";

/** A read-only link with a copy button; clicking the field selects it for a manual copy. */
export function LinkField({ url, label, small }: { url: string; label: string; small?: boolean }) {
  const t = useT();
  const toast = useToast();
  return (
    <span className={"ev-link" + (small ? " ev-link--sm" : "")}>
      <input readOnly value={url} aria-label={label} onClick={(e) => e.currentTarget.select()} onFocus={(e) => e.currentTarget.select()} />
      <Button
        variant="ghost"
        size="icon"
        aria-label={label}
        title={label}
        icon={<Copy size={ICON.sm} aria-hidden />}
        onClick={async (e) => {
          const input = e.currentTarget.previousElementSibling as HTMLInputElement | null;
          if (!(await copyText(url))) {
            input?.select();
            document.execCommand("copy");
          }
          toast.ok(t.t("Link copied"));
        }}
      />
    </span>
  );
}

/** "expired" once past, "expires in Nd" inside a week; nothing otherwise. */
export function ExpiryHint({ expiresAt }: { expiresAt: string | undefined }) {
  const t = useT();
  const exp = toDate(expiresAt);
  if (!exp) return null;
  const ms = exp.getTime() - Date.now();
  const when = fmtDateTime(expiresAt, t.current());
  if (ms <= 0) return <span className="ev-expiry is-expired" title={t.t("Expired {{when}}", { when })}>{t.t("expired")}</span>;
  const days = Math.ceil(ms / 86_400_000);
  if (days <= 7) return <span className="ev-expiry" title={t.t("Expires {{when}}", { when })}>{t.t("expires in {{n}}d", { n: days })}</span>;
  return null;
}

const csvCell = (c: string) => (/[",\n]/.test(c) ? `"${c.replace(/"/g, '""')}"` : c);

/**
 * Copy every team's links at once, for pasting into a spreadsheet, a
 * chat or an email merge: captain links as "team<TAB>url", or all of
 * them, player links too, as CSV.
 */
export function useCopyLinks(teams: Team[]) {
  const t = useT();
  const toast = useToast();
  const captains = async () => {
    const txt = teams.map((tm) => `${tm.name}\t${teamURL(tm)}`).join("\n");
    if (await copyText(txt)) toast.ok(t.t("Copied {{n}} captain links", { n: teams.length }));
    else toast.err(t.t("Clipboard blocked — select and copy by hand"));
  };
  const csv = async () => {
    const lines = [["team", "member", "url"]];
    for (const tm of teams) {
      lines.push([tm.name, "(captain)", teamURL(tm)]);
      for (const m of members(tm)) lines.push([tm.name, m.name || "", memberURL(m.token)]);
    }
    const text = lines.map((r) => r.map(csvCell).join(",")).join("\n");
    if (await copyText(text)) return toast.ok(t.t("Copied {{n}} rows", { n: lines.length - 1 }));
    // Mobile Safari and locked-down browsers refuse the clipboard: hand over a file instead.
    try {
      downloadBlob(new Blob([text], { type: "text/csv" }), "team-links.csv");
      toast.ok(t.t("Downloaded team-links.csv — open it in your sharing app."));
    } catch {
      toast.err(t.t("Clipboard blocked — select and copy by hand"));
    }
  };
  return { captains, csv };
}

/**
 * The public live view: its link, to open on the venue's screen, and
 * whether it shows the teams on a map (off by default — positions give
 * puzzles away).
 */
export function LiveViewDialog({ event, onChanged, onClose }: { event: EventInfo; onChanged: () => void; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const [showMap, setShowMap] = useState(event.show_map);
  const [busy, setBusy] = useState(false);
  const url = liveURL(event);
  const toggle = async (on: boolean) => {
    setShowMap(on);
    setBusy(true);
    try {
      await api.patchEvent(event.id, { show_map: on });
      toast.ok(on ? t.t("Map enabled on live view") : t.t("Map hidden"));
      onChanged();
    } catch (e) {
      setShowMap(!on);
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Live view")}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t.t("Close")}</Button>
          <a className="btn primary" href={url} target="_blank" rel="noopener">
            {t.t("Open")}
            <ExternalLink size={ICON.sm} aria-hidden />
          </a>
        </>
      }
    >
      <div className="ev-live-dlg">
        <p className="ev-muted">{t.t("A leaderboard for the venue's big screen. Open the link on any screen — no login needed.")}</p>
        <div className="ev-row-sm"><LinkField url={url} label={t.t("Copy link")} /></div>
        <label className="ev-check">
          <input type="checkbox" checked={showMap} disabled={busy} onChange={(e) => void toggle(e.target.checked)} />
          {t.t("Show real-time map")}
        </label>
      </div>
    </Dialog>
  );
}
