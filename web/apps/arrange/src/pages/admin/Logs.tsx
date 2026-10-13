import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowDown, Pause, Play, Search, Trash2 } from "lucide-react";
import { isGlobalAdmin } from "@zonventure/api";
import { useSession } from "../../session.tsx";
import { useT } from "../../i18n.tsx";
import { Button, ErrorBox, ICON, Page, PageHeader, Pill, errorText, useToast } from "../../ui/index.ts";
import { listLogs, setLogLevel, type LogEntry } from "./api.ts";
import "./admin.css";

const LEVELS = ["DEBUG", "INFO", "WARN", "ERROR"] as const;
const MAX_BUFFER = 5000;
const rank = (l: string) => ({ DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 })[String(l || "INFO").toUpperCase()] ?? 1;

function matches(e: LogEntry, needle: string): boolean {
  if ((e.msg || "").toLowerCase().includes(needle) || stamp(e.ts).includes(needle)) return true;
  return (e.attrs ?? []).some((a) => String(a.k ?? "").toLowerCase().includes(needle) || attrText(a.v).toLowerCase().includes(needle));
}

function attrText(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string") return /[\s="]/.test(v) ? JSON.stringify(v) : v;
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

const pad = (n: number) => String(n).padStart(2, "0");

/** An entry's time in the browser's zone, as the audit view shows it, so one clock time finds both. */
function stamp(ts: string): string {
  const d = new Date(ts);
  if (!ts || Number.isNaN(d.getTime())) return ts || "";
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

const ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;

/** A consumed one-shot filter the audit view leaves for "logs around this time". */
function takePrefilter(): string {
  try {
    const pf = sessionStorage.getItem("zv_logs_prefilter");
    if (pf) sessionStorage.removeItem("zv_logs_prefilter");
    return pf ?? "";
  } catch {
    return "";
  }
}

/**
 * The server's log, live: the buffered backlog, then /ws/logs as it
 * happens. The server level sets what the server records; "Show" and
 * the filter only narrow what this page shows, and reapply to the
 * lines already here. Pause freezes the view while entries keep
 * arriving underneath.
 */
export function LogsPage() {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  // Support reads the log but leaves the server level alone.
  const full = state.status === "authenticated" && isGlobalAdmin(state.user);
  const buffer = useRef<(LogEntry & { key: number })[]>([]);
  const nextKey = useRef(0);
  const lastSeq = useRef(0);
  const [, setTick] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [serverLevel, setServerLevel] = useState("INFO");
  const [minLevel, setMinLevel] = useState("DEBUG");
  const [filter, setFilter] = useState(takePrefilter);
  const [pausedAt, setPausedAt] = useState<number | null>(null);
  const [live, setLive] = useState(false);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const [atBottom, setAtBottom] = useState(true);

  // Many entries can land in one frame; paint once per frame.
  const frame = useRef(0);
  const repaint = useCallback(() => {
    if (frame.current) return;
    frame.current = requestAnimationFrame(() => {
      frame.current = 0;
      setTick((n) => n + 1);
    });
  }, []);

  // A backfill skips what is already here; a live entry is always new
  // (its seq starts over when the server restarts).
  const ingest = useCallback((list: LogEntry[], dedupe: boolean) => {
    const fresh = dedupe ? list.filter((e) => !e.seq || e.seq > lastSeq.current) : list;
    if (fresh.length === 0) return;
    const b = buffer.current;
    for (const e of fresh) b.push({ ...e, key: nextKey.current++ });
    if (b.length > MAX_BUFFER) b.splice(0, b.length - MAX_BUFFER);
    const tail = fresh[fresh.length - 1]!;
    if (tail.seq) lastSeq.current = tail.seq;
    repaint();
  }, [repaint]);

  useEffect(() => {
    let disposed = false;
    let ws: WebSocket | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let backoff = 500;
    const ac = new AbortController();

    // After a reconnect, catch up on whatever the socket missed.
    const backfill = async () => {
      try {
        const r = await listLogs(ac.signal, lastSeq.current);
        if (disposed) return;
        setServerLevel(r.level || "INFO");
        ingest(r.entries ?? [], true);
        setError(null);
      } catch (e) {
        if (!disposed) setError(errorText(e));
      }
    };
    let opened = false;
    const connect = () => {
      if (disposed) return;
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      const sock = new WebSocket(`${proto}//${location.host}/ws/logs`);
      ws = sock;
      sock.onopen = () => {
        backoff = 500;
        setLive(true);
        if (opened) void backfill();
        opened = true;
      };
      sock.onmessage = (ev) => {
        let m: { kind?: string; data?: unknown };
        try { m = JSON.parse(String(ev.data)); } catch { return; }
        if (m?.kind === "entry" && m.data) ingest([m.data as LogEntry], false);
      };
      sock.onclose = () => {
        setLive(false);
        if (disposed) return;
        const delay = Math.min(backoff, 10_000) + Math.floor(Math.random() * 500);
        backoff = Math.min(backoff * 2, 10_000);
        timer = setTimeout(connect, delay);
      };
    };
    void backfill().then(connect);
    return () => {
      disposed = true;
      ac.abort();
      clearTimeout(timer);
      ws?.close();
      cancelAnimationFrame(frame.current);
      frame.current = 0;
    };
  }, [ingest]);

  const needle = filter.trim().toLowerCase();
  const floor = rank(minLevel);
  const all = buffer.current;
  const upto = pausedAt === null ? all.length : Math.min(pausedAt, all.length);
  const shown = all.slice(0, upto).filter((e) => rank(e.level) >= floor && (!needle || matches(e, needle)));
  const waiting = all.length - upto;

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  });

  const onScroll = () => {
    const el = scroller.current;
    if (!el) return;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    stick.current = bottom;
    if (bottom !== atBottom) setAtBottom(bottom);
  };

  const changeServerLevel = async (level: string) => {
    const before = serverLevel;
    setServerLevel(level);
    try {
      const r = await setLogLevel(level);
      setServerLevel(r.level);
    } catch (e) {
      setServerLevel(before);
      toast.err(e);
    }
  };

  const clear = () => {
    buffer.current = [];
    if (pausedAt !== null) setPausedAt(0);
    setTick((n) => n + 1);
  };

  const jump = () => {
    stick.current = true;
    setAtBottom(true);
    const el = scroller.current;
    if (el) el.scrollTop = el.scrollHeight;
  };

  return (
    <div className="adm-logs">
      <Page wide>
        <PageHeader
          title={t.t("Logs")}
          subtitle={t.t("Recent entries since the last restart. The server level goes back to INFO on restart. For older lines, run docker compose logs app on the host.")}
          badges={live ? <Pill tone="good">{t.t("Streaming")}</Pill> : <Pill tone="warn">{t.t("Reconnecting…")}</Pill>}
          actions={
            <label className="adm-logbar__sel">
              <span>{t.t("Server level")}</span>
              <select value={serverLevel} disabled={!full} onChange={(e) => void changeServerLevel(e.target.value)} title={t.t("Server log level")}>
                {LEVELS.map((l) => <option key={l} value={l}>{l}</option>)}
              </select>
            </label>
          }
        />
        {error && <ErrorBox message={error} />}
        <div className="adm-logbar" role="toolbar" aria-label={t.t("Logs")}>
          <label className="adm-logbar__sel">
            <span>{t.t("Show")}</span>
            <select value={minLevel} onChange={(e) => setMinLevel(e.target.value)} title={t.t("Client-side minimum level")}>
              {LEVELS.map((l) => <option key={l} value={l}>{l}</option>)}
            </select>
          </label>
          <label className="adm-search adm-logbar__filter">
            <Search size={ICON.md} aria-hidden />
            <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t.t("Filter (substring)")} aria-label={t.t("Filter (substring)")} />
          </label>
          <Button
            size="sm"
            icon={pausedAt === null ? <Pause size={ICON.sm} aria-hidden /> : <Play size={ICON.sm} aria-hidden />}
            aria-pressed={pausedAt !== null}
            onClick={() => setPausedAt(pausedAt === null ? all.length : null)}
          >
            {pausedAt === null ? t.t("Pause") : waiting > 0 ? t.t("Resume ({{n}} new)", { n: waiting }) : t.t("Resume")}
          </Button>
          {all.length > 0 && <Button size="sm" icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={clear}>{t.t("Clear")}</Button>}
          <span style={{ color: "var(--dz-muted)", fontSize: "var(--dz-text-xs)" }}>{t.t("Times in {{zone}}", { zone: ZONE })}</span>
        </div>
        <div className="adm-term-wrap">
          <div className="adm-term" ref={scroller} onScroll={onScroll} role="log" aria-live="off" aria-label={t.t("Logs")} tabIndex={0}>
            {shown.length === 0 ? (
              <div className="adm-term__empty">{all.length === 0 ? t.t("Waiting for log entries…") : t.t("No entries match.")}</div>
            ) : (
              shown.map((e) => (
                <div key={e.key} className={`adm-term__line is-${String(e.level || "INFO").toLowerCase()}`}>
                  <span className="adm-term__ts" title={e.ts}>{stamp(e.ts)}</span> <span className="adm-term__lvl">{String(e.level || "INFO").padEnd(5)}</span> {e.msg}
                  {(e.attrs ?? []).map((a, j) => (
                    <span key={j}> <span className="adm-term__k">{a.k}</span>=<span className="adm-term__v">{attrText(a.v)}</span></span>
                  ))}
                </div>
              ))
            )}
          </div>
          {!atBottom && (
            <Button size="sm" variant="primary" className="adm-term__jump" icon={<ArrowDown size={ICON.sm} aria-hidden />} onClick={jump}>{t.t("Jump to latest")}</Button>
          )}
        </div>
      </Page>
    </div>
  );
}
