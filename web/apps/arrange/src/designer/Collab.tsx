import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Bot, Check, CheckCircle2, GitPullRequestArrow, MessageSquare, Pencil, RotateCcw, Send, SmilePlus, Trash2, X } from "lucide-react";
import { adventures } from "@zonventure/api";
import { blocksOf, nodeBySlug, type Graph, type GraphDiff, type Node } from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { PersonAvatar, relTime } from "../ui/index.ts";
import { peerColor } from "../ui/presence.tsx";
import { KINDS } from "./blocks/registry.tsx";
import { summarize } from "./blocks/summary.ts";
import { Button, ICON } from "./dz/ui.tsx";
import "./collab.css";

type Collab = adventures.Collab;
type Thread = adventures.Thread;
type StepStatus = adventures.StepStatus;
type T = ReturnType<typeof useT>;

/** The statuses in the order work goes through them, with their labels. */
export const STATUSES: { id: Exclude<StepStatus, "">; label: string }[] = [
  { id: "idea", label: "Idea" },
  { id: "draft", label: "Draft" },
  { id: "review", label: "Needs review" },
  { id: "ready", label: "Ready" },
];
export const statusLabel = (s: StepStatus) => STATUSES.find((x) => x.id === s)?.label ?? "No status";

const ASSISTANT = "assistant";

/**
 * The adventure's conversations, statuses and people, loaded once and
 * again whenever someone changes them (collab_changed on the socket).
 */
export function useCollab(slug: string, enabled: boolean) {
  const [collab, setCollab] = useState<Collab | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const load = useCallback(() => {
    adventures
      .getCollab(slug)
      .then(setCollab)
      .catch(() => {
        /* keep what is shown; the next change retries */
      });
  }, [slug]);
  useEffect(() => {
    if (enabled) load();
  }, [enabled, load]);
  // Several changes in a row reload once.
  const reload = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(load, 150);
  }, [load]);
  useEffect(() => () => void (timer.current && clearTimeout(timer.current)), []);
  return { collab, reload };
}

/** What a card shows of the work on its step. */
export interface CardTalk {
  open: number;
  status: StepStatus;
  owner?: string;
  ownerColor?: string;
  ownerAvatar?: string;
  /** Changed since this author last looked. */
  unseen?: boolean;
  /** Has an open thread that mentions this author. */
  mentionsMe?: boolean;
  /** Open suggestions on it. */
  suggestions?: number;
}

export function cardTalk(collab: Collab | null, unseen: ReadonlySet<string>, me: number): Map<string, CardTalk> {
  const out = new Map<string, CardTalk>();
  const get = (node: string) => {
    let v = out.get(node);
    if (!v) out.set(node, (v = { open: 0, status: "" }));
    return v;
  };
  for (const th of collab?.threads ?? []) {
    if (!th.node || th.resolved_at) continue;
    const v = get(th.node);
    v.open++;
    if (th.suggestion_state === "open") v.suggestions = (v.suggestions ?? 0) + 1;
    if (th.comments.some((c) => c.mentions.includes(me))) v.mentionsMe = true;
  }
  for (const s of collab?.steps ?? []) {
    const v = get(s.node);
    v.status = s.status;
    if (s.owner) {
      v.owner = s.owner.name.replace(/@.*/, "");
      v.ownerColor = peerColor({ user_id: s.owner.id, kind: "human" });
      if (s.owner.avatar) v.ownerAvatar = s.owner.avatar;
    }
  }
  for (const slug of unseen) get(slug).unseen = true;
  return out;
}

const who = (c: adventures.Comment, t: T) =>
  c.assistant ? t.t("{{name}}'s assistant", { name: (c.author?.name ?? "").replace(/@.*/, "") }) : (c.author?.name ?? t.t("Someone")).replace(/@.*/, "");

/** A comment's text, its @mentions picked out. */
function Body({ text, people }: { text: string; people: adventures.UserRef[] }) {
  const names = useMemo(() => new Set([ASSISTANT, ...people.map((p) => p.name.toLowerCase())]), [people]);
  const parts = text.split(/(@[\w.+@-]+)/g);
  return (
    <p className="talk__body">
      {parts.map((p, i) =>
        p.startsWith("@") && names.has(p.slice(1).replace(/[.,;:!?)]+$/, "").toLowerCase()) ? (
          <span key={i} className={"talk__mention" + (p.slice(1).toLowerCase().startsWith(ASSISTANT) ? " is-assistant" : "")}>
            {p}
          </span>
        ) : (
          p
        ),
      )}
    </p>
  );
}

/**
 * A text box for a comment that offers the people — and the assistant —
 * as the author types @. Ctrl/Cmd+Enter sends.
 */
function Composer({
  people,
  placeholder,
  initial = "",
  submitLabel,
  onSubmit,
  onCancel,
  autoFocus,
  extra,
}: {
  people: adventures.UserRef[];
  placeholder: string;
  initial?: string;
  submitLabel: string;
  onSubmit: (body: string) => Promise<void>;
  onCancel?: () => void;
  autoFocus?: boolean;
  extra?: ReactNode;
}) {
  const t = useT();
  const [text, setText] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pick, setPick] = useState(0);
  const area = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState(0);
  // Where the caret goes once a picked name is in the text: set in the
  // same commit, before the next key can land where React left it.
  const placeCaret = useRef<number | null>(null);
  useLayoutEffect(() => {
    const at = placeCaret.current;
    if (at === null) return;
    placeCaret.current = null;
    area.current?.focus();
    area.current?.setSelectionRange(at, at);
  });

  // The @word being typed, if the caret is in one.
  const typing = useMemo(() => {
    const before = text.slice(0, caret);
    const m = /(?:^|\s)@([\w.+@-]*)$/.exec(before);
    return m ? { query: m[1]!.toLowerCase(), start: caret - m[1]!.length - 1 } : null;
  }, [text, caret]);
  const options = useMemo(() => {
    if (!typing) return [];
    const all: { name: string; assistant: boolean; id: number; avatar?: string | undefined }[] = [
      { name: ASSISTANT, assistant: true, id: 0 },
      ...people.map((p) => ({ name: p.name, assistant: false, id: p.id, avatar: p.avatar })),
    ];
    return all.filter((o) => o.name.toLowerCase().includes(typing.query)).slice(0, 6);
  }, [typing, people]);
  useEffect(() => setPick(0), [typing?.query]);

  const insert = (name: string) => {
    if (!typing) return;
    const next = text.slice(0, typing.start) + "@" + name + " " + text.slice(caret);
    const at = typing.start + name.length + 2;
    placeCaret.current = at;
    setText(next);
    setCaret(at);
  };
  const send = async () => {
    if (!text.trim() || busy) return;
    setBusy(true);
    setError(null);
    try {
      await onSubmit(text.trim());
      setText("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="talk__composer">
      <div className="talk__input">
        <textarea
          ref={area}
          className="field__input"
          rows={2}
          value={text}
          placeholder={placeholder}
          autoFocus={autoFocus}
          onChange={(e) => {
            setText(e.target.value);
            setCaret(e.target.selectionStart);
          }}
          onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
          onKeyDown={(e) => {
            if (options.length) {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                setPick((p) => (p + (e.key === "ArrowDown" ? 1 : options.length - 1)) % options.length);
                return;
              }
              if (e.key === "Enter" || e.key === "Tab") {
                e.preventDefault();
                insert(options[pick]!.name);
                return;
              }
            }
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void send();
            }
            if (e.key === "Escape" && onCancel) onCancel();
          }}
          aria-label={placeholder}
        />
        {options.length > 0 && (
          <ul className="talk__suggest" role="listbox">
            {options.map((o, i) => (
              <li key={o.name}>
                <button
                  type="button"
                  role="option"
                  aria-selected={i === pick}
                  className={i === pick ? "is-picked" : ""}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    insert(o.name);
                  }}
                >
                  {o.assistant ? <Bot size={ICON.sm} aria-hidden /> : <PersonAvatar name={o.name} src={o.avatar} size={22} color={peerColor({ user_id: o.id, kind: "human" })} />}
                  {o.assistant ? t.t("The assistant") : o.name}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
      {error && <p className="field__error">{error}</p>}
      <div className="talk__composer-foot">
        {extra}
        <span className="talk__spacer" />
        {onCancel && (
          <Button size="sm" variant="ghost" onClick={onCancel}>
            {t.t("Cancel")}
          </Button>
        )}
        <Button size="sm" variant="primary" disabled={!text.trim()} loading={busy} onClick={() => void send()} title={t.t("Ctrl+Enter")}>
          <Send size={ICON.sm} />
          {submitLabel}
        </Button>
      </div>
    </div>
  );
}

/**
 * The reactions on a comment, each a toggle, and a picker for one more.
 * Hovering one says who. They acknowledge without a reply.
 */
export function Reactions({ slug, target, collab, me, onChanged }: { slug: string; target: string; collab: Collab; me: number; onChanged: () => void }) {
  const t = useT();
  const [picking, setPicking] = useState(false);
  const here = collab.reactions.filter((r) => r.target === target);
  const name = (id: number) => (collab.people.find((p) => p.id === id)?.name ?? t.t("someone")).replace(/@.*/, "");
  const toggle = (emoji: string) => {
    setPicking(false);
    void adventures.toggleReaction(slug, target, emoji).then(onChanged);
  };
  return (
    <div className="react">
      {here.map((r) => (
        <button
          key={r.emoji}
          type="button"
          className={"react__pill" + (r.users.includes(me) ? " is-mine" : "")}
          onClick={() => toggle(r.emoji)}
          title={r.users.map(name).join(", ")}
          aria-pressed={r.users.includes(me)}
        >
          <span aria-hidden>{r.emoji}</span>
          {r.users.length}
        </button>
      ))}
      <span className="react__add-wrap">
        <button type="button" className="react__add" onClick={() => setPicking((v) => !v)} aria-label={t.t("React")} title={t.t("React")} aria-expanded={picking}>
          <SmilePlus size={ICON.sm} />
        </button>
        {picking && (
          <span className="react__picker" role="menu">
            {adventures.REACTIONS.map((e) => (
              <button key={e} type="button" role="menuitem" onClick={() => toggle(e)} aria-label={e}>
                {e}
              </button>
            ))}
          </span>
        )}
      </span>
    </div>
  );
}

/** A settings key's name for a person. */
const FIELD: Record<string, string> = {
  answer: "Answer",
  hints: "Hints",
  responses: "Replies to wrong answers",
  outputs: "Ways out",
  zone: "Place",
  time_limit_seconds: "Time limit",
  skip_after_guesses: "Skipping",
  skip_after_seconds: "Skipping",
  lockout_seconds: "Lockout",
  repeatable: "Repeatable",
  once: "Shown once",
  fixed: "Fixed layout",
  outcome: "Outcome",
};

interface FieldChange {
  label: string;
  before?: string;
  after?: string;
}

/** What a proposed step changes, field by field, for people to read. */
function changesOf(base: Node, proposed: Node, graph: Graph, t: T): FieldChange[] {
  const out: FieldChange[] = [];
  if ((base.title ?? "") !== (proposed.title ?? "")) out.push({ label: t.t("Title"), before: base.title ?? "", after: proposed.title ?? "" });
  if (base.type !== proposed.type) out.push({ label: t.t("Kind of step"), before: base.type, after: proposed.type });
  const was = new Map(blocksOf(base).map((b) => [b.id, b]));
  const now = new Map(blocksOf(proposed).map((b) => [b.id, b]));
  for (const [id, b] of now) {
    const old = was.get(id);
    if (!old) out.push({ label: t.t("Added: {{block}}", { block: t.t(KINDS[b.kind]?.label ?? b.kind) }), after: summarize(b, graph, t, proposed) });
    else if (stable(old) !== stable(b)) {
      let before = summarize(old, graph, t, base);
      let after = summarize(b, graph, t, proposed);
      // A summary shows a text's start; a change further on would read the same twice.
      const was = "text" in old ? old.text : undefined;
      const is = "text" in b ? b.text : undefined;
      if (before === after && typeof was === "string" && typeof is === "string") [before, after] = aroundChange(was, is);
      out.push({ label: t.t(KINDS[b.kind]?.label ?? b.kind), before, after });
    }
  }
  for (const [id, b] of was) if (!now.has(id)) out.push({ label: t.t("Removed: {{block}}", { block: t.t(KINDS[b.kind]?.label ?? b.kind) }), before: summarize(b, graph, t, base) });
  // Every other setting, by name.
  const cfg = (n: Node) => (n.puzzle ?? n.story ?? n.end ?? n.hub ?? n.gate ?? n.effect ?? n.switch ?? {}) as Record<string, unknown>;
  const a = cfg(base);
  const b = cfg(proposed);
  const seen = new Set<string>();
  for (const k of new Set([...Object.keys(a), ...Object.keys(b)])) {
    if (k === "blocks" || stable(a[k]) === stable(b[k])) continue;
    const label = t.t(FIELD[k] ?? k.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase()));
    if (seen.has(label)) continue;
    seen.add(label);
    out.push({ label, before: show(a[k]), after: show(b[k]) });
  }
  return out;
}

/** Two texts cut to where they differ, with a little of what is around it. */
function aroundChange(a: string, b: string, context = 30): [string, string] {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let end = 0;
  while (end < a.length - start && end < b.length - start && a[a.length - 1 - end] === b[b.length - 1 - end]) end++;
  const from = Math.max(0, start - context);
  const cut = (s: string) => {
    const to = Math.min(s.length, s.length - end + context);
    return (from > 0 ? "…" : "") + s.slice(from, to) + (to < s.length ? "…" : "");
  };
  return [cut(a), cut(b)];
}

/** JSON with object keys sorted: two copies of a step compare equal however they were built. */
function stable(v: unknown): string {
  return JSON.stringify(v, (_k, val: unknown) =>
    val && typeof val === "object" && !Array.isArray(val)
      ? Object.fromEntries(Object.entries(val as Record<string, unknown>).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)))
      : val,
  );
}

function show(v: unknown): string {
  if (v === undefined || v === null) return "";
  if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") return String(v);
  const s = JSON.stringify(v);
  return s.length > 80 ? s.slice(0, 80) + "…" : s;
}

/** A suggestion's change, before and after, and — open — accept or reject. */
function SuggestionView({
  slug,
  thread,
  graph,
  onAccept,
  onChanged,
}: {
  slug: string;
  thread: Thread;
  graph: Graph;
  onAccept?: ((th: Thread) => Promise<void>) | undefined;
  onChanged: () => void;
}) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const sg = thread.suggestion as { base: Node; proposed: Node };
  const changes = useMemo(() => changesOf(sg.base, sg.proposed, graph, t), [sg, graph, t]);
  const current = nodeBySlug(graph, thread.node);
  const moved = current && stable({ ...current, x: 0, y: 0 }) !== stable({ ...sg.base, x: 0, y: 0 });
  const state = thread.suggestion_state;
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      onChanged();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className={`suggest suggest--${state ?? "open"}`}>
      <div className="suggest__head">
        <GitPullRequestArrow size={ICON.sm} aria-hidden />
        <strong>{state === "accepted" ? t.t("Suggestion accepted") : state === "rejected" ? t.t("Suggestion declined") : t.t("Suggested change")}</strong>
      </div>
      <dl className="suggest__changes">
        {changes.map((c, i) => (
          <div key={i} className="suggest__change">
            <dt>{c.label}</dt>
            <dd>
              {c.before !== undefined && c.before !== "" && <del>{c.before}</del>}
              {c.after !== undefined && c.after !== "" && <ins>{c.after}</ins>}
            </dd>
          </div>
        ))}
      </dl>
      {state === "open" && (
        <>
          {!current && <p className="field__hint">{t.t("The step was removed, so this can only be declined.")}</p>}
          {current && moved && <p className="field__hint">{t.t("The step changed since. Accepting merges the suggestion field by field.")}</p>}
          {error && <p className="field__error">{error}</p>}
          <div className="suggest__actions">
            {current && onAccept && (
              <Button size="sm" variant="primary" loading={busy} onClick={() => void act(() => onAccept(thread))}>
                <Check size={ICON.sm} />
                {t.t("Accept")}
              </Button>
            )}
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => void act(() => adventures.decideSuggestion(slug, thread.id, "rejected"))}>
              {t.t("Decline")}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

/** One conversation: its comments, a reply, resolve. */
function ThreadView({
  slug,
  thread,
  collab,
  me,
  graph,
  onChanged,
  heading,
  onAccept,
}: {
  slug: string;
  thread: Thread;
  collab: Collab;
  me: number;
  graph: Graph;
  onChanged: () => void;
  heading?: ReactNode;
  onAccept?: ((th: Thread) => Promise<void>) | undefined;
}) {
  const t = useT();
  const [replying, setReplying] = useState(false);
  const [editing, setEditing] = useState<number | null>(null);
  const resolved = !!thread.resolved_at;
  const node = thread.node ? nodeBySlug(graph, thread.node) : undefined;
  const block = node && thread.block_id ? blocksOf(node).find((b) => b.id === thread.block_id) : undefined;
  const locale = t.current();
  return (
    <article className={"talk__thread" + (resolved ? " is-resolved" : "") + (thread.for_assistant && !resolved ? " is-task" : "")}>
      {(heading || block || (thread.for_assistant && !resolved)) && (
        <header className="talk__thread-head">
          {heading}
          {block && node && <span className="talk__about">{t.t("About: {{block}}", { block: blockName(block, graph, node, t) })}</span>}
          {thread.for_assistant && !resolved && (
            <span className="talk__task">
              <Bot size={ICON.xs} aria-hidden />
              {t.t("Task for the assistant")}
            </span>
          )}
        </header>
      )}
      {thread.suggestion && <SuggestionView slug={slug} thread={thread} graph={graph} onAccept={onAccept} onChanged={onChanged} />}
      {thread.comments.map((c) => (
        <div key={c.id} className={"talk__comment" + (c.assistant ? " is-assistant" : "")}>
          {c.assistant ? (
            <span className="talk__avatar" style={{ "--peer": peerColor({ user_id: c.author?.id ?? 0, kind: "assistant" }) } as React.CSSProperties} aria-hidden>
              <Bot size={12} />
            </span>
          ) : (
            <PersonAvatar name={c.author?.name ?? "?"} src={c.author?.avatar} size={24} color={peerColor({ user_id: c.author?.id ?? 0, kind: "human" })} />
          )}
          <div className="talk__comment-main">
            <div className="talk__meta">
              <strong>{who(c, t)}</strong>
              <span className="talk__when" title={new Date(c.created_at).toLocaleString(locale)}>
                {relTime(c.created_at, locale)}
                {c.edited_at && ` · ${t.t("edited")}`}
              </span>
              {c.author?.id === me && !c.assistant && editing !== c.id && (
                <span className="talk__own">
                  <button type="button" onClick={() => setEditing(c.id)} aria-label={t.t("Edit")} title={t.t("Edit")}>
                    <Pencil size={ICON.xs} />
                  </button>
                  <button
                    type="button"
                    onClick={() => void adventures.deleteComment(slug, c.id).then(onChanged)}
                    aria-label={t.t("Delete")}
                    title={t.t("Delete")}
                  >
                    <Trash2 size={ICON.xs} />
                  </button>
                </span>
              )}
            </div>
            {editing === c.id ? (
              <Composer
                people={collab.people}
                placeholder={t.t("Edit the comment")}
                initial={c.body}
                submitLabel={t.t("Save")}
                autoFocus
                onCancel={() => setEditing(null)}
                onSubmit={async (body) => {
                  await adventures.editComment(slug, c.id, body);
                  setEditing(null);
                  onChanged();
                }}
              />
            ) : (
              <Body text={c.body} people={collab.people} />
            )}
            <Reactions slug={slug} target={`comment:${c.id}`} collab={collab} me={me} onChanged={onChanged} />
          </div>
        </div>
      ))}
      {resolved && !thread.suggestion && (
        <p className="talk__resolved">
          <CheckCircle2 size={ICON.sm} aria-hidden />
          {t.t("Resolved by {{name}}", { name: (thread.resolved_by?.name ?? t.t("someone")).replace(/@.*/, "") })}
        </p>
      )}
      {replying ? (
        <Composer
          people={collab.people}
          placeholder={t.t("Reply… @ to mention")}
          submitLabel={t.t("Reply")}
          autoFocus
          onCancel={() => setReplying(false)}
          onSubmit={async (body) => {
            await adventures.replyToThread(slug, thread.id, body);
            setReplying(false);
            onChanged();
          }}
        />
      ) : (
        <div className="talk__actions">
          <Button size="sm" variant="ghost" onClick={() => setReplying(true)}>
            {t.t("Reply")}
          </Button>
          {thread.suggestion_state === "open" ? null : resolved ? (
            <Button size="sm" variant="ghost" onClick={() => void adventures.resolveThread(slug, thread.id, false).then(onChanged)}>
              <RotateCcw size={ICON.sm} />
              {t.t("Reopen")}
            </Button>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => void adventures.resolveThread(slug, thread.id, true).then(onChanged)}>
              <Check size={ICON.sm} />
              {t.t("Resolve")}
            </Button>
          )}
        </div>
      )}
    </article>
  );
}

function blockName(b: ReturnType<typeof blocksOf>[number], graph: Graph, node: Node, t: T) {
  const text = summarize(b, graph, t, node).trim();
  const label = t.t(KINDS[b.kind]?.label ?? b.kind);
  return text ? `${label}: ${text.length > 40 ? text.slice(0, 40) + "…" : text}` : label;
}

/**
 * The work on one step, under its settings in the inspector: how far
 * along it is and who has it, and the conversations about it. It stays
 * usable when the settings are read-only — someone else holding the
 * step is a reason to talk, not to stop.
 */
export function StepTalk({
  slug,
  node,
  graph,
  collab,
  me,
  onChanged,
  onSuggest,
  onAccept,
}: {
  slug: string;
  node: Node;
  graph: Graph;
  collab: Collab | null;
  me: number;
  onChanged: () => void;
  /** Start proposing a change instead of editing; absent while proposing. */
  onSuggest?: (() => void) | undefined;
  onAccept?: ((th: Thread) => Promise<void>) | undefined;
}) {
  const t = useT();
  const [about, setAbout] = useState("");
  const [showResolved, setShowResolved] = useState(false);
  if (!collab) return null;
  const threads = collab.threads.filter((th) => th.node === node.slug);
  const open = threads.filter((th) => !th.resolved_at);
  const done = threads.filter((th) => th.resolved_at);
  const state = collab.steps.find((s) => s.node === node.slug);
  const blocks = blocksOf(node);
  const set = (change: { status?: StepStatus; owner_id?: number }) => void adventures.setStepStatus(slug, node.slug, change).then(onChanged);

  return (
    <section className="talk" aria-label={t.t("Work and discussion")}>
      <div className="talk__work">
        <label className="talk__field">
          <span className="field__label">{t.t("Status")}</span>
          <select className={`field__input talk__status talk__status--${state?.status || "none"}`} value={state?.status ?? ""} onChange={(e) => set({ status: e.target.value as StepStatus })}>
            <option value="">{t.t("No status")}</option>
            {STATUSES.map((s) => (
              <option key={s.id} value={s.id}>
                {t.t(s.label)}
              </option>
            ))}
          </select>
        </label>
        <label className="talk__field">
          <span className="field__label">{t.t("Owner")}</span>
          <select className="field__input" value={state?.owner?.id ?? 0} onChange={(e) => set({ owner_id: Number(e.target.value) })}>
            <option value={0}>{t.t("Nobody")}</option>
            {collab.people.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
                {p.id === me ? ` (${t.t("you")})` : ""}
              </option>
            ))}
          </select>
        </label>
      </div>

      {onSuggest && (
        <div className="talk__suggest-start">
          <Button size="sm" variant="ghost" onClick={onSuggest} title={t.t("Edit a copy and send it as a suggestion, for whoever has the step to accept")}>
            <GitPullRequestArrow size={ICON.sm} />
            {t.t("Suggest a change")}
          </Button>
        </div>
      )}

      <h3 className="talk__title">
        <MessageSquare size={ICON.sm} aria-hidden />
        {t.t("Discussion")}
        {open.length > 0 && <span className="talk__count">{open.length}</span>}
      </h3>
      {open.map((th) => (
        <ThreadView key={th.id} slug={slug} thread={th} collab={collab} me={me} graph={graph} onChanged={onChanged} onAccept={onAccept} />
      ))}
      <Composer
        people={collab.people}
        placeholder={t.t("Comment on this step… @ to mention, @assistant to ask the assistant")}
        submitLabel={t.t("Comment")}
        onSubmit={async (body) => {
          await adventures.startThread(slug, node.slug, body, about);
          setAbout("");
          onChanged();
        }}
        extra={
          blocks.length > 1 ? (
            <select className="field__input talk__about-pick" value={about} onChange={(e) => setAbout(e.target.value)} aria-label={t.t("About")}>
              <option value="">{t.t("About the whole step")}</option>
              {blocks
                .filter((b) => b.kind !== "title")
                .map((b) => (
                  <option key={b.id} value={b.id}>
                    {blockName(b, graph, node, t)}
                  </option>
                ))}
            </select>
          ) : undefined
        }
      />
      {done.length > 0 && (
        <>
          <button type="button" className="talk__toggle" onClick={() => setShowResolved((v) => !v)}>
            {showResolved ? t.t("Hide resolved") : t.t("Show {{n}} resolved", { n: done.length })}
          </button>
          {showResolved &&
            done.map((th) => <ThreadView key={th.id} slug={slug} thread={th} collab={collab} me={me} graph={graph} onChanged={onChanged} onAccept={onAccept} />)}
        </>
      )}
    </section>
  );
}

type Filter = "open" | "mine" | "assistant" | "suggestions" | "resolved";

/**
 * Every conversation on the adventure, and its steps by status — the
 * right-hand slot's "Work" view.
 */
export function WorkPanel({
  slug,
  graph,
  collab,
  me,
  onChanged,
  onGo,
  onClose,
  onAccept,
}: {
  slug: string;
  graph: Graph;
  collab: Collab | null;
  me: number;
  onChanged: () => void;
  onGo: (node: string) => void;
  onClose: () => void;
  onAccept?: ((th: Thread) => Promise<void>) | undefined;
}) {
  const t = useT();
  const [tab, setTab] = useState<"threads" | "board">("threads");
  const [filter, setFilter] = useState<Filter>("open");
  const titleOf = (slugOf: string) => {
    const n = nodeBySlug(graph, slugOf);
    return n ? n.title || n.slug : t.t("{{step}} (removed)", { step: slugOf });
  };
  const threads = (collab?.threads ?? []).filter((th) => {
    switch (filter) {
      case "open":
        return !th.resolved_at;
      case "mine":
        return !th.resolved_at && th.comments.some((c) => c.mentions.includes(me));
      case "assistant":
        return !th.resolved_at && th.for_assistant;
      case "suggestions":
        return th.suggestion_state === "open";
      case "resolved":
        return !!th.resolved_at;
    }
  });
  const counts = {
    open: (collab?.threads ?? []).filter((th) => !th.resolved_at).length,
    mine: (collab?.threads ?? []).filter((th) => !th.resolved_at && th.comments.some((c) => c.mentions.includes(me))).length,
    assistant: (collab?.threads ?? []).filter((th) => !th.resolved_at && th.for_assistant).length,
    suggestions: (collab?.threads ?? []).filter((th) => th.suggestion_state === "open").length,
  };
  return (
    <div className="work">
      <div className="panel-head">
        <div className="work__tabs" role="tablist">
          <button type="button" role="tab" aria-selected={tab === "threads"} className={tab === "threads" ? "is-on" : ""} onClick={() => setTab("threads")}>
            {t.t("Discussion")}
            {counts.open > 0 && <span className="talk__count">{counts.open}</span>}
          </button>
          <button type="button" role="tab" aria-selected={tab === "board"} className={tab === "board" ? "is-on" : ""} onClick={() => setTab("board")}>
            {t.t("Board")}
          </button>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label={t.t("Close")} title={t.t("Close")}>
          <X size={ICON.md} />
        </Button>
      </div>
      <div className="work__body">
        {!collab ? null : tab === "threads" ? (
          <>
            <div className="work__filters">
              {(
                [
                  ["open", t.t("Open"), counts.open],
                  ["mine", t.t("Mentions me"), counts.mine],
                  ["assistant", t.t("For the assistant"), counts.assistant],
                  ["suggestions", t.t("Suggestions"), counts.suggestions],
                  ["resolved", t.t("Resolved"), 0],
                ] as const
              ).map(([id, label, n]) => (
                <button key={id} type="button" className={"work__chip" + (filter === id ? " is-on" : "")} onClick={() => setFilter(id)}>
                  {label}
                  {n > 0 && <span className="talk__count">{n}</span>}
                </button>
              ))}
            </div>
            {threads.length === 0 && <p className="work__empty">{filter === "open" ? t.t("No open conversations. Comment on a step in its inspector, or on the whole adventure below.") : t.t("Nothing here.")}</p>}
            {threads.map((th) => (
              <ThreadView
                key={th.id}
                slug={slug}
                thread={th}
                collab={collab}
                me={me}
                graph={graph}
                onChanged={onChanged}
                onAccept={onAccept}
                heading={
                  th.node ? (
                    <button type="button" className="talk__step" onClick={() => onGo(th.node)}>
                      {titleOf(th.node)}
                    </button>
                  ) : (
                    <span className="talk__step is-adventure">{t.t("Whole adventure")}</span>
                  )
                }
              />
            ))}
            <Composer
              people={collab.people}
              placeholder={t.t("Comment on the whole adventure… @ to mention")}
              submitLabel={t.t("Comment")}
              onSubmit={async (body) => {
                await adventures.startThread(slug, "", body);
                onChanged();
              }}
            />
          </>
        ) : (
          <Board slug={slug} graph={graph} collab={collab} onChanged={onChanged} onGo={onGo} />
        )}
      </div>
    </div>
  );
}

/** The steps in columns by status; a card dragged to a column takes its status. */
function Board({ slug, graph, collab, onChanged, onGo }: { slug: string; graph: Graph; collab: Collab; onChanged: () => void; onGo: (node: string) => void }) {
  const t = useT();
  const [over, setOver] = useState<string | null>(null);
  const status = new Map(collab.steps.map((s) => [s.node, s]));
  const open = new Map<string, number>();
  for (const th of collab.threads) if (th.node && !th.resolved_at) open.set(th.node, (open.get(th.node) ?? 0) + 1);
  const steps = graph.nodes.filter((n) => n.type !== "start");
  const columns: { id: StepStatus; label: string }[] = [{ id: "", label: "No status" }, ...STATUSES];
  return (
    <div className="board">
      {columns.map((col) => {
        const here = steps.filter((n) => (status.get(n.slug)?.status ?? "") === col.id);
        return (
          <section
            key={col.id || "none"}
            className={`board__col board__col--${col.id || "none"}` + (over === col.id ? " is-over" : "")}
            onDragOver={(e) => {
              e.preventDefault();
              setOver(col.id);
            }}
            onDragLeave={() => setOver((o) => (o === col.id ? null : o))}
            onDrop={(e) => {
              e.preventDefault();
              setOver(null);
              const node = e.dataTransfer.getData("text/x-step");
              if (node && (status.get(node)?.status ?? "") !== col.id) void adventures.setStepStatus(slug, node, { status: col.id }).then(onChanged);
            }}
          >
            <h4 className="board__head">
              {t.t(col.label)}
              <span className="board__n">{here.length}</span>
            </h4>
            {here.map((n) => {
              const s = status.get(n.slug);
              return (
                <button
                  key={n.slug}
                  type="button"
                  className="board__card"
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData("text/x-step", n.slug);
                    e.dataTransfer.effectAllowed = "move";
                  }}
                  onClick={() => onGo(n.slug)}
                >
                  <span className="board__title">{n.title || (n.type === "end" ? t.t("End") : n.slug)}</span>
                  <span className="board__meta">
                    {(open.get(n.slug) ?? 0) > 0 && (
                      <span className="board__talk">
                        <MessageSquare size={ICON.xs} aria-hidden />
                        {open.get(n.slug)}
                      </span>
                    )}
                    {s?.owner && (
                      <span title={s.owner.name}>
                        <PersonAvatar name={s.owner.name} src={s.owner.avatar} size={22} color={peerColor({ user_id: s.owner.id, kind: "human" })} />
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </section>
        );
      })}
    </div>
  );
}

/** What happened since the author last looked. */
export interface CatchUp {
  since: string;
  diff: GraphDiff;
  comments: number;
  mentions: number;
}

export function catchUpOf(seen: { seen_at: string; graph: Graph } | null, draft: Graph, collab: Collab, me: number, diff: (a: Graph, b: Graph) => GraphDiff): CatchUp | null {
  if (!seen) return null;
  const at = Date.parse(seen.seen_at);
  const d = diff(seen.graph, draft);
  // An author's own assistant is not the author: its comments are news.
  const newer = collab.threads.flatMap((th) => th.comments).filter((c) => Date.parse(c.created_at) > at && (c.assistant || c.author?.id !== me));
  const out = { since: seen.seen_at, diff: d, comments: newer.length, mentions: newer.filter((c) => c.mentions.includes(me)).length };
  const changes = d.added.length + d.removed.length + d.changed.length + d.resources.length;
  return changes + out.comments > 0 ? out : null;
}

export function CatchUpBanner({ catchUp, onShowTalk, onDismiss }: { catchUp: CatchUp; onShowTalk: () => void; onDismiss: () => void }) {
  const t = useT();
  const { diff } = catchUp;
  const parts: string[] = [];
  if (diff.added.length) parts.push(t.t(diff.added.length === 1 ? "1 step added" : "{{n}} steps added", { n: diff.added.length }));
  if (diff.changed.length) parts.push(t.t(diff.changed.length === 1 ? "1 step changed" : "{{n}} steps changed", { n: diff.changed.length }));
  if (diff.removed.length) parts.push(t.t(diff.removed.length === 1 ? "1 step removed" : "{{n}} steps removed", { n: diff.removed.length }));
  if (diff.resources.length) parts.push(t.t("places or items changed"));
  return (
    <div className="editor-conn-banner editor-conn-banner--info catchup" role="status">
      <span className="editor-conn-banner-text">
        <strong>{t.t("Since you were here {{when}}:", { when: relTime(catchUp.since, t.current()) })}</strong> {parts.join(", ")}
        {catchUp.comments > 0 && (
          <>
            {parts.length > 0 && " · "}
            <Button variant="link" onClick={onShowTalk}>
              {t.t(catchUp.comments === 1 ? "1 new comment" : "{{n}} new comments", { n: catchUp.comments })}
              {catchUp.mentions > 0 && ` (${t.t(catchUp.mentions === 1 ? "1 mentions you" : "{{n}} mention you", { n: catchUp.mentions })})`}
            </Button>
          </>
        )}
        {parts.length > 0 && <span className="catchup__hint"> {t.t("New and changed steps have a dot until you open them.")}</span>}
      </span>
      <Button size="sm" variant="ghost" onClick={onDismiss} aria-label={t.t("Dismiss")}>
        <X size={ICON.sm} />
      </Button>
    </div>
  );
}
