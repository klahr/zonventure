import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import {
  ArrowLeft,
  Bot,
  Check,
  GitCompare,
  History,
  Info,
  LayoutGrid,
  ListOrdered,
  Loader2,
  MoreVertical,
  Network,
  Play,
  Plus,
  Redo2,
  Rocket,
  RotateCcw,
  Tag,
  Undo2,
  UploadCloud,
  X,
} from "lucide-react";
import { ApiError, adventures, type play } from "@zonventure/api";
import {
  FORMAT_V2,
  canRedo,
  canUndo,
  cleanGraph,
  addNode,
  diffGraphs,
  differs,
  mergeGraphs,
  emptyGraph,
  moveNode,
  neighbour,
  newNode,
  nodeBySlug,
  portLabel,
  rebase,
  record,
  redo,
  removeNode,
  STEP_PITCH,
  tidy,
  undo,
  updateNode,
  emptyHistory,
  type AdventureMeta,
  type Direction,
  type Envelope,
  type Graph,
  type GraphDiff,
  type HistoryState,
  type Issue,
  type Report,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { href } from "../router.ts";
import { useAdventureWatch } from "../ui/live.ts";
import { PresenceBar, holderOf, peerColor, usePresence, type Peer } from "../ui/presence.tsx";
import { Canvas, cardSizes, type LiveFx } from "./Canvas.tsx";
import type { CanvasLive } from "./Live.tsx";
import type { Mark } from "./NodeCard.tsx";
import { Inspector } from "./Inspector.tsx";
import { LanguageSwitch, LanguagesSettings, TranslateStep, translationLanguages } from "./Translate.tsx";
import { ListView } from "./ListView.tsx";
import { Resources } from "./Resources.tsx";
import { Simulator, type SimMode } from "./Simulator.tsx";
import { FloatingWindow } from "./FloatingWindow.tsx";
import { useInspectorWidth } from "./InspectorResize.tsx";
import { TopBarPortal, useTopBarTitle } from "../topbar.tsx";
import { Look } from "./Look.tsx";
import { StepSearch } from "./StepSearch.tsx";
import { AdventureSlug } from "./MediaEditor.tsx";
import { CanvasContextMenu, type ContextMenuItem } from "./dz/CanvasContextMenu.tsx";
import { DiffDialog, HelpModal, PanelModal, PublishCelebration, PublishLabelModal } from "./dz/dialogs.tsx";
import { Button, ICON, IssueRow, IssuesButton, Notice, PromptModal } from "./dz/ui.tsx";
import { isNarrow as narrowScreen, isTouch } from "./device.ts";
import "./dz.css";
import "./designer.css";


const errorText = (e: unknown): string => (e instanceof Error ? e.message : String(e));

/** How long steps someone else changed stay highlighted. */
const CHANGED_MS = 8000;
/** How long arrivals, removals and new wires animate. */
const FX_MS = 1400;
/** How long, and how many, of others' doings the ticker shows. */
const TICKER_MS = 20_000;
const TICKER_MAX = 4;

/** Who saved what someone else did: graph_saved, and their peer. */
interface Saver {
  saved_at: string;
  by: string;
  user_id: number;
  assistant: boolean;
  touched?: string[];
  note?: string;
  /** Their presence peer, when known: whom following follows. */
  peer: string;
}

interface TickerEntry {
  id: number;
  at: number;
  color: string;
  assistant: boolean;
  text: string;
  note: string;
  /** The step to go to. */
  slug: string | null;
}

/** The steps of `next` that are new or differ from `prev`. */
function changedSlugs(prev: Graph | null, next: Graph): Set<string> {
  const before = new Map((prev?.nodes ?? []).map((n) => [n.slug, JSON.stringify(n)]));
  return new Set(next.nodes.filter((n) => before.get(n.slug) !== JSON.stringify(n)).map((n) => n.slug));
}

const loadError = (e: unknown): Loaded => ({
  state: "error",
  message: errorText(e),
  ...(e instanceof ApiError ? { status: e.status } : {}),
});

type Loaded =
  | { state: "loading" }
  | { state: "error"; message: string; status?: number }
  | { state: "none" } // adventure has no graph yet
  | { state: "ready" };

type Published = { version: number; label?: string } | null;

/** A version being looked at instead of the draft (read-only). */
type Preview = { version: number; graph: Graph };

/** What the right-hand slot shows, dazyflow-style: one slot, many uses. */
type Slot = "inspect" | "history";

type PublishAsk = { kind: "first" | "update" } | { kind: "rollback"; version: number };

/** Autosave debounce, as dazyflow's useAutosave. */
const AUTOSAVE_MS = 1500;
const CELEBRATION_MS = 1200;

const portLabelFor = (g: Graph, slug: string, port: string) => {
  const n = nodeBySlug(g, slug);
  return n ? portLabel(n, port) : port;
};

const envelope = (g: Graph): Envelope => ({ format: FORMAT_V2, graph: cleanGraph(g) });

/**
 * The adventure designer, laid out and behaving like dazyflow's flow
 * editor so a dazyflow user is at home: one toolbar over the canvas, a
 * 320 px slot on the right for the inspector, version history or the
 * preview, issue counts that open panels, and the draft → publish flow.
 *
 * The draft autosaves. It may be incomplete; errors show as counts and
 * card badges but never block saving. Publishing is the gate: it refuses
 * a draft with errors, names the release, and is what events start on.
 * Version history previews any release, restores it into the draft, or
 * rolls the published pointer back to it.
 */
export function Designer({ slug }: { slug: string }) {
  const t = useT();
  const [loaded, setLoaded] = useState<Loaded>({ state: "loading" });
  const [history, setHistory] = useState<HistoryState>(emptyHistory);
  // The graph as last autosaved, to know when to save again.
  const [saved, setSaved] = useState<Graph | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [published, setPublished] = useState<Published>(null);
  const [serverDirty, setServerDirty] = useState(false);
  const [issues, setIssues] = useState<Issue[]>([]);
  // The saved_at of the draft as this page last loaded or saved it. Saves
  // send it, so this page never overwrites a later save by someone else —
  // an assistant building the adventure, another tab.
  const since = useRef<string | undefined>(undefined);
  // A save was refused because someone else saved first.
  const [conflict, setConflict] = useState(false);
  // Steps someone else just changed, highlighted for a moment in their colour.
  const [changed, setChanged] = useState<Map<string, string>>(() => new Map());
  // Their arrivals, removals and wires, animated; fxSeq counts the saves shown.
  const [fx, setFx] = useState<LiveFx | null>(null);
  const [fxSeq, setFxSeq] = useState(0);
  // What others did lately, in words.
  const [ticker, setTicker] = useState<TickerEntry[]>([]);
  // Steps both this page and someone else changed: this page's kept.
  const [clash, setClash] = useState<{ steps: string[]; by: string } | null>(null);
  // Steps someone else has open that the author chose to edit anyway.
  const [takenOver, setTakenOver] = useState<Set<string>>(() => new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [slot, setSlot] = useState<Slot>("inspect");
  const [issuePanel, setIssuePanel] = useState<"error" | "warning" | null>(null);

  const [versions, setVersions] = useState<adventures.GraphVersion[] | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [labelEditing, setLabelEditing] = useState<adventures.GraphVersion | null>(null);
  const [publishAsk, setPublishAsk] = useState<PublishAsk | null>(null);
  const [publishing, setPublishing] = useState(false);
  const [celebrate, setCelebrate] = useState(false);
  const [diff, setDiff] = useState<{ loading: boolean; diff: GraphDiff | null } | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [kebab, setKebab] = useState<{ x: number; y: number } | null>(null);

  const [simMode, setSimMode] = useState<SimMode>("step");
  const [simRestart, setSimRestart] = useState(0);
  const [playing, setPlaying] = useState<string | null>(null);
  // The preview floats beside the inspector, so a step can be edited and
  // watched at once.
  const [previewing, setPreviewing] = useState(false);
  // On a phone the outline is the way in: a canvas cannot be worked with
  // a thumb. Elsewhere the canvas is, and the outline is a toggle away.
  const [listView, setListView] = useState(() => narrowScreen());
  const [paletteRequest, setPaletteRequest] = useState(0);
  const [focusRequest, setFocusRequest] = useState(0);
  const [revealRequest, setRevealRequest] = useState(0);

  // The adventure's palette, edited in Adventure settings.
  const [brandSaved, setBrandSaved] = useState<play.Brand>({});
  const [brand, setBrand] = useState<play.Brand>({});
  const [brandSaving, setBrandSaving] = useState(false);
  const [brandError, setBrandError] = useState<string | null>(null);
  const [adventureTitle, setAdventureTitle] = useState(slug);
  // The adventure's own language and texts, which its translations cover
  // too; they live on the adventure, not in the graph.
  const [adventureLang, setAdventureLang] = useState("");
  const [meta, setMeta] = useState<AdventureMeta>({});
  // Puzzles from the classic editor: null until known, "unknown" when
  // they could not be read.
  const [lanePuzzles, setLanePuzzles] = useState<number | "unknown" | null>(null);
  const brandDirty = useMemo(() => JSON.stringify(brand) !== JSON.stringify(brandSaved), [brand, brandSaved]);

  const draft = history.present;
  // What the canvas shows: an older version while previewing, else the draft.
  const graph = preview?.graph ?? draft;
  const readOnly = !!preview;
  // The language the author is editing the text in; the main one edits
  // everything, another only its translations.
  // The server writes the adventure's language into every graph it
  // stores; a graph not saved yet takes it from the adventure.
  const mainLang = graph?.language || adventureLang;
  const [editLangPick, setEditLang] = useState("");
  const editLang = graph && translationLanguages(graph, mainLang).includes(editLangPick) ? editLangPick : mainLang;
  const translating = !!graph && editLang !== mainLang;

  // --- load -------------------------------------------------------------------

  const adopt = useCallback((r: adventures.GraphPayload<Envelope, Report> | null, g: Graph, savedGraph: Graph | null) => {
    setHistory(rebase(g, Date.now()));
    setSaved(savedGraph);
    since.current = r?.saved_at;
    setIssues(r?.report.issues ?? []);
    setPublished(r?.published ?? null);
    setServerDirty(!!r?.dirty);
    setLoaded({ state: "ready" });
  }, []);

  useEffect(() => {
    const ac = new AbortController();
    adventures
      .getGraph<Envelope, Report>(slug, ac.signal)
      .then((r) => {
        if (ac.signal.aborted) return;
        if (!r) setLoaded({ state: "none" });
        else adopt(r, r.envelope.graph, r.envelope.graph);
      })
      .catch((e: unknown) => {
        if (!ac.signal.aborted) setLoaded(loadError(e));
      });
    adventures
      .getLook(slug, ac.signal)
      .then(({ title, intro, outro, language, lanePuzzles: n, brand: b }) => {
        if (title) setAdventureTitle(title);
        setLanePuzzles(n);
        setAdventureLang(language);
        setMeta({ title, intro, outro });
        setBrandSaved(b);
        setBrand(b);
      })
      .catch(() => {
        // No palette is the default look; not knowing the puzzles means
        // offering both ways to start.
        setLanePuzzles("unknown");
      });
    return () => ac.abort();
  }, [slug, adopt]);

  // A new adventure has nothing to start from but a blank flow, so it
  // opens on one instead of asking.
  useEffect(() => {
    if (loaded.state === "none" && lanePuzzles === 0) adopt(null, emptyGraph(), null);
  }, [loaded.state, lanePuzzles, adopt]);

  const startFromLanes = async () => {
    setLoaded({ state: "loading" });
    try {
      const r = await adventures.convertGraph<Envelope, Report>(slug);
      adopt(r, tidy(r.envelope.graph), null);
    } catch (e) {
      setLoaded(loadError(e));
    }
  };

  // --- editing -------------------------------------------------------------------

  const change = useCallback(
    (g: Graph) => {
      if (readOnly) return;
      setHistory((h) => record(h, g, Date.now()));
    },
    [readOnly],
  );
  // "a": the next step, from the keyboard — the quick-add palette after the
  // selected step, as Ctrl+K opens it — or the Start first, when the
  // graph has none.
  const addNext = useCallback(() => {
    if (readOnly || !graph) return;
    if (graph.nodes.some((n) => n.type === "start")) {
      setListView(false);
      setPaletteRequest((n) => n + 1);
      return;
    }
    const x = Math.min(0, ...graph.nodes.map((n) => n.x ?? 0)) - (graph.nodes.length ? STEP_PITCH : 0);
    const start = newNode(graph, "start", { x, y: 0 });
    change(addNode(graph, start));
    setSelected(start.slug);
  }, [readOnly, graph, change]);
  const doUndo = useCallback(() => setHistory((h) => undo(h)?.state ?? h), []);
  const doRedo = useCallback(() => setHistory((h) => redo(h)?.state ?? h), []);

  // --- autosave ------------------------------------------------------------------

  // Comparing serialises both graphs, so it runs when they change, not on
  // every render — presence, the ticker and saving flags render often.
  const unsaved = useMemo(() => !!draft && draft !== saved && (saved === null || differs(draft, saved)), [draft, saved]);
  // The latest draft and its saved base, for merging someone else's save
  // into it from a socket callback.
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const savedRef = useRef(saved);
  savedRef.current = saved;
  // Someone else saved while this page's own save was on its way.
  const missed = useRef<Saver | null>(null);

  // --- someone else's saves ---------------------------------------------------------

  /**
   * Shows what someone else just did: the steps they changed glow in
   * their colour, new ones pop in, removed ones fade out where they were,
   * new wires draw themselves, and the ticker says it in words.
   */
  const show = useCallback(
    (prev: Graph | null, next: Graph, who: Saver) => {
      const color = peerColor({ user_id: who.user_id, kind: who.assistant ? "assistant" : "human" });
      const touched = changedSlugs(prev, next);
      const before = new Set((prev?.nodes ?? []).map((n) => n.slug));
      const after = new Set(next.nodes.map((n) => n.slug));
      const arrived = new Set([...after].filter((x) => !before.has(x)));
      const leaving = (prev?.nodes ?? []).filter((n) => !after.has(n.slug));
      const wires = new Set((prev?.edges ?? []).map((e) => `${e.from}:${e.from_port}>${e.to}`));
      const wired = new Set(next.edges.filter((e) => !wires.has(`${e.from}:${e.from_port}>${e.to}`)).map((e) => `${e.from}:${e.from_port}`));
      setChanged(new Map([...touched].map((x) => [x, color])));
      setFx({ arrived, leaving, wired });
      setFxSeq((n) => n + 1);

      const title = (g: Graph | null, x: string) => g?.nodes.find((n) => n.slug === x)?.title || x;
      const name = who.assistant ? t.t("{{name}}'s assistant", { name: who.by }) : who.by;
      const edited = [...touched].filter((x) => !arrived.has(x));
      const text =
        arrived.size + leaving.length + edited.length === 0
          ? wired.size
            ? t.t("{{name}} rewired the flow", { name })
            : t.t("{{name}} saved", { name })
          : arrived.size + leaving.length + edited.length > 1
            ? t.t("{{name}} changed {{n}} steps", { name, n: arrived.size + leaving.length + edited.length })
            : arrived.size
              ? t.t("{{name}} added “{{step}}”", { name, step: title(next, [...arrived][0]!) })
              : leaving.length
                ? t.t("{{name}} removed “{{step}}”", { name, step: leaving[0]!.title || leaving[0]!.slug })
                : t.t("{{name}} changed “{{step}}”", { name, step: title(next, edited[0]!) });
      const go = [...arrived][0] ?? edited[0] ?? null;
      setTicker((ts) => [{ id: Date.now() + Math.random(), at: Date.now(), color, assistant: who.assistant, text, note: who.note || "", slug: go }, ...ts].slice(0, TICKER_MAX));
      if (go && followRef.current && followRef.current === who.peer) setFlyTo((f) => ({ slug: go, seq: (f?.seq ?? 0) + 1 }));
    },
    [t],
  );

  /**
   * Loads the draft someone else saved, as an ordinary edit that can be
   * undone. Unsaved work of this page's own is merged into it step by
   * step — field by field within a step — so both people's edits stay;
   * only where both changed the same thing is the author's own kept and
   * the clash pointed out.
   */
  const pullRemote = useCallback(
    async (who: Saver | null) => {
      const r = await adventures.getGraph<Envelope, Report>(slug).catch(() => null);
      if (!r) return false;
      const theirs = r.envelope.graph;
      const mine = draftRef.current;
      const base = savedRef.current;
      let next = theirs;
      if (mine && base && differs(mine, base)) {
        const m = mergeGraphs(base, mine, theirs);
        next = m.graph;
        if (m.conflicts.length) setClash({ steps: m.conflicts, by: who?.by ?? "" });
      }
      if (who) show(mine, next, who);
      setHistory((h) => record(h, next, Date.now()));
      setSaved(theirs);
      since.current = r.saved_at;
      setIssues(r.report.issues ?? []);
      setPublished(r.published ?? null);
      setServerDirty(!!r.dirty);
      setConflict(false);
      return true;
    },
    [slug, show],
  );

  const saveNow = useCallback(async (): Promise<boolean> => {
    if (!draft) return false;
    setSaving(true);
    missed.current = null;
    try {
      const r = await adventures.saveGraph<Envelope, Report>(slug, envelope(draft), since.current);
      setSaved(draft);
      since.current = r.saved_at;
      setIssues(r.report.issues ?? []);
      setPublished(r.published ?? null);
      setServerDirty(!!r.dirty);
      setSaveError(null);
      const m = missed.current as Saver | null;
      if (m && m.saved_at !== r.saved_at) void pullRemote(m);
      return true;
    } catch (e) {
      // Someone saved first: take theirs in, merged with this page's
      // work, and the next autosave stores both.
      if (e instanceof ApiError && e.code === "draft_changed") {
        if (!(await pullRemote(missed.current))) setConflict(true);
      } else setSaveError(errorText(e));
      return false;
    } finally {
      missed.current = null;
      setSaving(false);
    }
  }, [draft, slug, pullRemote]);

  useEffect(() => {
    if (!unsaved || readOnly || conflict || loaded.state !== "ready") return;
    const id = setTimeout(() => void saveNow(), AUTOSAVE_MS);
    return () => clearTimeout(id);
  }, [unsaved, readOnly, conflict, loaded.state, saveNow]);

  const presence = usePresence();
  const busy = useRef({ saving, readOnly });
  busy.current = { saving, readOnly };
  const live = useAdventureWatch(loaded.state === "ready" ? slug : null, (m) => {
    if (presence.take(m) || m.kind !== "graph_saved") return;
    const d = m.data as Omit<Saver, "peer">;
    if (d.saved_at === since.current) return; // this page's own save
    const who: Saver = { ...d, peer: d.assistant ? `a${d.user_id}` : (presence.others.find((p) => p.user_id === d.user_id && p.kind === "human")?.id ?? "") };
    const b = busy.current;
    if (b.readOnly) return;
    // While this page's own save is on its way, it answers first: if it
    // is refused, it pulls; if not, this save came after it.
    if (b.saving) missed.current = who;
    else void pullRemote(who);
  });

  // This page's step, for the others to see.
  const focus = slot === "inspect" && selected ? selected : "";
  useEffect(() => {
    if (live.state === "live") live.send("focus", { node: focus });
  }, [focus, live]);

  // Following someone: the canvas keeps to their view, or flies to each
  // step an assistant works on. Following ends when they leave, or when
  // the author moves the canvas or presses Escape.
  const [following, setFollowing] = useState<string | null>(null);
  const [flyTo, setFlyTo] = useState<{ slug: string; seq: number } | null>(null);
  const followRef = useRef(following);
  followRef.current = following;
  const followed = presence.others.find((p) => p.id === following) ?? null;
  useEffect(() => {
    if (following && !followed) setFollowing(null);
  }, [following, followed]);
  useEffect(() => {
    if (!following) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setFollowing(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [following]);
  const follow = useCallback((p: Peer | null) => {
    setFollowing(p?.id ?? null);
    if (p) setListView(false);
    const at = p?.focus ?? p?.touched?.[0];
    if (p?.kind === "assistant" && at) setFlyTo((f) => ({ slug: at, seq: (f?.seq ?? 0) + 1 }));
  }, []);
  const stopFollowing = useCallback(() => setFollowing(null), []);
  // Following mirrors their inspector too: the step they have open opens
  // here — read-only, as they hold it — and closes when they close it.
  // An assistant's step is the one it last worked on.
  const followedStep = followed ? (followed.focus ?? (followed.kind === "assistant" ? followed.touched?.[0] : undefined) ?? null) : null;
  useEffect(() => {
    if (!following) return;
    setSelected(followedStep);
    if (followedStep) setSlot("inspect");
  }, [following, followedStep]);
  // The author picking or closing a step themselves stops following.
  const choose = useCallback((x: string | null) => {
    if (followRef.current) setFollowing(null);
    setSelected(x);
  }, []);
  const canvasLive = useMemo<CanvasLive>(
    () => ({ peers: presence.others, pointers: presence.pointers, send: live.send, following, flyTo, onUserMove: stopFollowing }),
    [presence.others, presence.pointers, live.send, following, flyTo, stopFollowing],
  );

  // Who has which step: open in their inspector, or an assistant's work.
  const marks = useMemo(() => {
    const out = new Map<string, Mark[]>();
    for (const p of presence.others) {
      const at = p.kind === "assistant" ? [...new Set([p.focus, ...(p.touched ?? [])])] : [p.focus];
      const mark = { id: p.id, name: p.name.replace(/@.*/, ""), color: peerColor(p), assistant: p.kind === "assistant" };
      for (const x of at) if (x) out.set(x, [...(out.get(x) ?? []), mark]);
    }
    return out;
  }, [presence.others]);

  useEffect(() => {
    if (!changed.size) return;
    const id = setTimeout(() => setChanged(new Map()), CHANGED_MS);
    return () => clearTimeout(id);
  }, [changed]);
  useEffect(() => {
    if (!fx) return;
    const id = setTimeout(() => setFx(null), FX_MS);
    return () => clearTimeout(id);
  }, [fx]);
  useEffect(() => {
    if (!ticker.length) return;
    const id = setTimeout(() => setTicker((ts) => ts.filter((x) => Date.now() - x.at < TICKER_MS)), 1000);
    return () => clearTimeout(id);
  }, [ticker]);

  // Validation between autosaves, so badges follow the author quickly —
  // only for the author's own edits: a draft just loaded, saved or pulled
  // in already came with its report.
  useEffect(() => {
    if (!draft || readOnly || !unsaved) return;
    const ac = new AbortController();
    const id = setTimeout(() => {
      adventures
        .validateGraph<Envelope, Report>(slug, envelope(draft), ac.signal)
        .then((r) => setIssues(r.report.issues ?? []))
        .catch(() => {
          /* keep the last report rather than flash "all clear" */
        });
    }, 400);
    return () => {
      clearTimeout(id);
      ac.abort();
    };
  }, [draft, readOnly, unsaved, slug]);

  useEffect(() => {
    if (!unsaved) return;
    const onBefore = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onBefore);
    return () => window.removeEventListener("beforeunload", onBefore);
  }, [unsaved]);

  // --- publish ----------------------------------------------------------------------

  // A step just added is left alone while the author fills it in: its
  // problems — no prompt yet, no answer — are what "new" means, and a
  // card that opens covered in red reads as a mistake. They appear once
  // the author moves on to another step. The toolbar's counts still
  // include them, so nothing publishes unnoticed.
  const [fresh, setFresh] = useState<string | null>(null);
  const knownSlugs = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (!draft) return;
    const now = new Set(draft.nodes.map((n) => n.slug));
    const before = knownSlugs.current;
    knownSlugs.current = now;
    const added = before ? [...now].filter((x) => !before.has(x)) : [];
    if (added.length === 1) setFresh(added[0]!);
  }, [draft]);
  useEffect(() => {
    if (fresh && selected !== fresh) setFresh(null);
  }, [selected, fresh]);
  const shownIssues = fresh ? issues.filter((i) => i.node !== fresh) : issues;

  const errors = issues.filter((i) => i.severity === "error");
  const warnings = issues.filter((i) => i.severity === "warning");
  const dirty = published === null ? true : serverDirty || unsaved;

  const doPublish = async (ask: PublishAsk, label: string) => {
    setPublishAsk(null);
    setPublishing(true);
    try {
      let r: adventures.GraphPayload<Envelope, Report>;
      if (ask.kind === "rollback") {
        r = await adventures.publishVersion<Envelope, Report>(slug, ask.version, label);
      } else {
        // Publish exactly what the author sees: save first if needed.
        if (unsaved && !(await saveNow())) return;
        r = await adventures.publishGraph<Envelope, Report>(slug, label);
      }
      setPublished(r.published ?? null);
      setServerDirty(!!r.dirty);
      setIssues(r.report.issues ?? []);
      setSaveError(null);
      setCelebrate(true);
      setTimeout(() => setCelebrate(false), CELEBRATION_MS);
      if (versions) void loadVersions();
    } catch (e) {
      const report = adventures.refusedReport<Report>(e);
      if (report) {
        setIssues(report.issues ?? []);
        setIssuePanel("error");
        setSaveError(t.t("Not published: fix the errors first."));
      } else {
        setSaveError(errorText(e));
      }
    } finally {
      setPublishing(false);
    }
  };

  const openDiff = async () => {
    if (!published || !draft) return;
    setDiff({ loading: true, diff: null });
    try {
      const r = await adventures.getVersion<Envelope, Report>(slug, published.version);
      setDiff({ loading: false, diff: diffGraphs(r.envelope.graph, draft) });
    } catch {
      setDiff({ loading: false, diff: null });
    }
  };

  // --- history ------------------------------------------------------------------------

  const loadVersions = useCallback(async () => {
    try {
      setVersions((await adventures.listVersions(slug)).versions);
    } catch {
      setVersions([]);
    }
  }, [slug]);

  const openHistory = () => {
    setSlot("history");
    setVersions(null);
    void loadVersions();
  };

  const previewVersion = async (v: number) => {
    try {
      const r = await adventures.getVersion<Envelope, Report>(slug, v);
      setPreview({ version: v, graph: r.envelope.graph });
      setSelected(null);
    } catch (e) {
      setSaveError(errorText(e));
    }
  };

  // Restore makes the old version the new draft — undoable, like any edit.
  const restore = () => {
    if (!preview) return;
    const g = preview.graph;
    setPreview(null);
    setHistory((h) => record(h, g, Date.now()));
  };

  const saveLabel = async (v: adventures.GraphVersion, label: string) => {
    setLabelEditing(null);
    try {
      await adventures.labelVersion(slug, v.version, label);
      void loadVersions();
      if (published?.version === v.version) setPublished({ ...published, label });
    } catch (e) {
      setSaveError(errorText(e));
    }
  };

  // --- keyboard ----------------------------------------------------------------------

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const inField = (e.target as HTMLElement)?.closest?.("input, textarea, select, [contenteditable]");
      const mod = e.ctrlKey || e.metaKey;
      const k = e.key.toLowerCase();
      if (!mod && !inField && e.key === "?") {
        setHelpOpen(true);
        return;
      }
      if (!mod && !inField && e.key === "/") {
        e.preventDefault();
        setSearchOpen(true);
        return;
      }
      if (!mod && !inField && !e.altKey && e.key === "a") {
        e.preventDefault();
        addNext();
        return;
      }
      if (!mod) return;
      if (k === "s") {
        // Saving is automatic; Ctrl+S saves now rather than doing nothing.
        e.preventDefault();
        void saveNow();
      } else if (k === "k") {
        e.preventDefault();
        setListView(false);
        setPaletteRequest((n) => n + 1);
      } else if (!inField && k === "z" && !e.shiftKey) {
        e.preventDefault();
        doUndo();
      } else if (!inField && (k === "y" || (k === "z" && e.shiftKey))) {
        e.preventDefault();
        doRedo();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [doUndo, doRedo, saveNow, addNext]);

  // h/j/k/l and the arrows move the selection along the flow; Shift+arrow
  // moves the selected step. Captured on the way down, so React Flow's own
  // arrow handling on a focused step never moves it as well.
  useEffect(() => {
    const dirs: Record<string, Direction> = {
      h: "prev", ArrowLeft: "prev", l: "next", ArrowRight: "next", j: "down", ArrowDown: "down", k: "up", ArrowUp: "up",
    };
    const steps: Record<string, [number, number]> = { ArrowLeft: [-20, 0], ArrowRight: [20, 0], ArrowUp: [0, -20], ArrowDown: [0, 20] };
    const onKey = (e: KeyboardEvent) => {
      const dir = dirs[e.key];
      if (!dir || !graph || e.ctrlKey || e.metaKey || e.altKey) return;
      if ((e.target as HTMLElement)?.closest?.("input, textarea, select, [contenteditable]")) return;
      if (document.querySelector('[aria-modal="true"]')) return;
      e.preventDefault();
      e.stopPropagation();
      if (e.shiftKey) {
        const n = selected ? nodeBySlug(graph, selected) : undefined;
        const d = steps[e.key];
        if (n && d && !readOnly) change(moveNode(graph, n.slug, (n.x ?? 0) + d[0], (n.y ?? 0) + d[1]));
        return;
      }
      const to = neighbour(graph, selected, dir);
      if (!to) return;
      setSelected(to);
      setRevealRequest((n) => n + 1);
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [graph, selected, readOnly, change]);

  // --- derived ----------------------------------------------------------------------

  const node = graph && selected ? nodeBySlug(graph, selected) : undefined;
  // Someone else with this step open holds it, softly: the inspector
  // shows it read-only until the author chooses to edit anyway.
  const holder =
    node && !readOnly && !takenOver.has(node.slug) ? holderOf(node.slug, presence.me, presence.others) : null;
  const reveal = (x: string) => {
    choose(x);
    setSlot("inspect");
    setFocusRequest((n) => n + 1);
  };
  const describePeer = (p: Peer) => {
    const at = p.focus ?? p.touched?.[0];
    const step = at && draft ? nodeBySlug(draft, at) : undefined;
    if (p.kind === "assistant") return p.note || (step ? t.t("Working on “{{step}}”", { step: step.title || step.slug }) : t.t("Working"));
    return step ? t.t("Editing “{{step}}”", { step: step.title || step.slug }) : t.t("Looking around");
  };
  const simEnvelope = useMemo(() => (draft ? envelope(draft) : null), [draft]);
  const slotOpen = slot !== "inspect" || !!node;
  useTopBarTitle(adventureTitle);
  const root = useRef<HTMLDivElement>(null);
  const inspectorWidth = useInspectorWidth(root, t.t("Resize the inspector"));

  const stage = useRef<HTMLDivElement>(null);
  const openStep = (slug: string) => {
    setSearchOpen(false);
    setSelected(slug);
    setSlot("inspect");
    setFocusRequest((n) => n + 1);
  };

  const jumpTo = (i: Issue) => {
    if (!i.node) return;
    setIssuePanel(null);
    setSelected(i.node);
    setSlot("inspect");
    setFocusRequest((n) => n + 1);
  };

  // --- render ----------------------------------------------------------------------

  if (loaded.state === "loading") return <p className="lede">{t.t("Loading…")}</p>;
  if (loaded.state === "error") {
    // A refusal is explained in words, with a way back; only an
    // unexpected failure shows the server's own message.
    const [title, body] =
      loaded.status === 403
        ? [t.t("You can't edit this adventure"), t.t("Only the organisation that owns an adventure can change it. Adventures from the shared catalog can be scheduled, but not edited.")]
        : loaded.status === 404
          ? [t.t("Adventure not found"), t.t("It may have been deleted or renamed.")]
          : [t.t("The designer could not open"), loaded.message];
    return (
      <div className="designer-empty" role="alert">
        <a className="link-btn" href={href({ name: "adventures" })}>
          ← {t.t("Adventures")}
        </a>
        <h1>{title}</h1>
        <p className="lede">{body}</p>
      </div>
    );
  }
  if (loaded.state === "none" && (lanePuzzles === null || lanePuzzles === 0)) return <p className="lede">{t.t("Loading…")}</p>;
  if (loaded.state === "none")
    return (
      <div className="designer-empty">
        <a className="link-btn" href={href({ name: "adventures" })}>
          ← {t.t("Adventures")}
        </a>
        <h1>{t.t("Design this adventure")}</h1>
        <p className="lede">{t.t("This adventure has puzzles from the classic editor. Turn them into a flow to go on with, or start from a blank canvas.")}</p>
        <div className="designer-empty__actions">
          <button type="button" className="btn btn--primary" onClick={() => void startFromLanes()}>
            {typeof lanePuzzles === "number" ? t.t("Start from its {{n}} puzzles", { n: lanePuzzles }) : t.t("Start from existing puzzles")}
          </button>
          <button type="button" className="btn btn--ghost" onClick={() => adopt(null, emptyGraph(), null)}>
            {t.t("Start blank")}
          </button>
        </div>
      </div>
    );
  if (!graph) return null;

  const isLive = published !== null;
  const issueRows = (list: Issue[]) =>
    list.map((i, k) => (
      <IssueRow key={k} text={<IssueText graph={graph} issue={i} />} actions={i.node ? <Button variant="link" onClick={() => jumpTo(i)}>{t.t("Show")}</Button> : undefined} />
    ));
  const kebabItems: ContextMenuItem[] = [
    { label: t.t("Adventure settings"), onClick: () => setSettingsOpen(true) },
    // A phone has no keyboard to take shortcuts from.
    ...(isTouch() ? [] : [{ label: t.t("Keyboard shortcuts"), shortcut: "?", onClick: () => setHelpOpen(true) }]),
  ];

  return (
    <div className="dz dz-designer" ref={root} style={inspectorWidth.style}>
      <div className="editor" data-has-selection={slotOpen ? "true" : "false"} data-inspector-expanded={slotOpen ? "true" : "false"}>
        <div className="canvas">
          <div className="editor-toolbar">
            <a className="dz-back" href={href({ name: "adventure", slug })} title={t.t("Back to the adventure")} aria-label={t.t("Back to the adventure")}>
              <ArrowLeft size={ICON.md} />
            </a>
            <span className="toolbar-divider" aria-hidden="true" />

            <div className="toolbar-scroll">
              <div className="toolbar-group">
                <Button variant="ghost" onClick={doUndo} disabled={!canUndo(history) || readOnly} title={t.t("Undo the last change (Ctrl/Cmd+Z)")} aria-label={t.t("Undo")}>
                  <Undo2 size={ICON.sm} />
                </Button>
                <Button variant="ghost" onClick={doRedo} disabled={!canRedo(history) || readOnly} title={t.t("Redo the change you undid (Ctrl/Cmd+Shift+Z)")} aria-label={t.t("Redo")}>
                  <Redo2 size={ICON.sm} />
                </Button>
              </div>
              <span className="toolbar-divider" aria-hidden="true" />
              <div className="toolbar-group">
                {/* The outline has its own add buttons, beneath its steps. */}
                {!listView && (
                <Button
                  variant="ghost"
                  className="editor-add-drop"
                  disabled={readOnly}
                  onClick={() => setPaletteRequest((n) => n + 1)}
                  title={t.t("Search and insert a step (Ctrl/Cmd+K)")}
                  aria-label={t.t("Add step")}
                >
                  <Plus size={ICON.sm} />
                  <span className="toolbar-label">{t.t("Add step")}</span>
                </Button>
                )}
                {/* Names where it takes you, so the label never contradicts
                    the view on screen. */}
                <Button
                  variant="ghost"
                  className="editor-dataview"
                  onClick={() => setListView((v) => !v)}
                  title={listView ? t.t("Back to the canvas") : t.t("Show the adventure as an outline of its steps")}
                  aria-label={listView ? t.t("Canvas view") : t.t("List view")}
                >
                  {listView ? <Network size={ICON.sm} /> : <ListOrdered size={ICON.sm} />}
                  <span className="toolbar-label">{listView ? t.t("Canvas view") : t.t("List view")}</span>
                </Button>
              </div>
              <span className="toolbar-divider" aria-hidden="true" />
              <div className="toolbar-group">
                {saving || unsaved ? (
                  <span className="editor-saving" title={t.t("Saving…")}>
                    <Loader2 size={ICON.sm} className="spin" />
                    <span className="toolbar-label">{t.t("Saving…")}</span>
                  </span>
                ) : (
                  <span className="editor-saved" title={t.t("Saved")}>
                    <Check size={ICON.sm} />
                    <span className="toolbar-label">{t.t("Saved")}</span>
                  </span>
                )}
                <Button variant="ghost" onClick={openHistory} title={t.t("Version history")} aria-label={t.t("History")}>
                  <History size={ICON.sm} />
                  <span className="toolbar-label">{t.t("History")}</span>
                </Button>
              </div>
            </div>

            <LanguageSwitch graph={graph} main={mainLang} meta={meta} value={editLang} onChange={setEditLang} />

            {!listView && (
              <Button variant="ghost" data-tidy="toolbar" disabled={readOnly} onClick={() => change(tidy(graph, cardSizes()))} title={t.t("Auto-arrange the adventure into clean columns")}>
                <LayoutGrid size={ICON.sm} aria-hidden="true" />
                <span className="toolbar-label">{t.t("Tidy")}</span>
              </Button>
            )}

            <IssuesButton
              kind="error"
              count={errors.length}
              title={t.plural(errors.length, { one: "{{n}} error", other: "{{n}} errors" })}
              heading={t.plural(errors.length, { one: "{{n}} error to fix before publishing", other: "{{n}} errors to fix before publishing" })}
              open={issuePanel === "error"}
              onOpenChange={(o) => setIssuePanel(o ? "error" : null)}
            >
              {issueRows(errors)}
            </IssuesButton>
            <IssuesButton
              kind="warning"
              count={warnings.length}
              title={t.plural(warnings.length, { one: "{{n}} warning", other: "{{n}} warnings" })}
              heading={t.plural(warnings.length, { one: "{{n}} warning", other: "{{n}} warnings" })}
              open={issuePanel === "warning"}
              onOpenChange={(o) => setIssuePanel(o ? "warning" : null)}
            >
              {issueRows(warnings)}
            </IssuesButton>

            {/* The one place to publish. A button, not a switch: publishing
                makes a version, it is not a setting to turn off again. With
                nothing to publish, it says so. */}
            <div className="editor-publish-group">
              {!isLive && (
                <Button
                  className={"editor-publish" + (celebrate ? " celebrate" : "")}
                  onClick={() => setPublishAsk({ kind: "first" })}
                  disabled={publishing || readOnly}
                  aria-label={t.t("Publish")}
                  title={readOnly ? t.t("Exit version preview before publishing.") : t.t("Publish this adventure so you can run it with players.")}
                >
                  <Rocket size={ICON.sm} />
                  <span className="toolbar-label">{publishing ? t.t("Publishing…") : t.t("Publish")}</span>
                </Button>
              )}
              {isLive && !dirty && (
                <span
                  className={"editor-live-chip" + (celebrate ? " celebrate" : "")}
                  role="status"
                  aria-label={t.t("Live")}
                  title={t.t("Published (version {{n}}). New events use this version; ones already running keep theirs.", { n: published.version })}
                >
                  <span className="editor-live-dot" aria-hidden="true" />
                  <span className="toolbar-label">{t.t("Live")}</span>
                </span>
              )}
              {isLive && dirty && (
                <>
                  <Button className="editor-publish" onClick={() => setPublishAsk({ kind: "update" })} disabled={publishing || readOnly} title={t.t("Publish your draft — events that start afterwards play it.")}>
                    <UploadCloud size={ICON.sm} />
                    <span className="toolbar-label">{publishing ? t.t("Publishing…") : t.t("Publish changes")}</span>
                  </Button>
                  <Button variant="ghost" onClick={() => void openDiff()} title={t.t("See what changed since the published version")} aria-label={t.t("Changes")}>
                    <GitCompare size={ICON.sm} />
                    <span className="toolbar-label">{t.t("Changes")}</span>
                  </Button>
                </>
              )}
            </div>

            <div className="toolbar-group">
              <Button
                variant="primary"
                className="editor-run"
                disabled={readOnly}
                onClick={() => {
                  setSimMode("run");
                  setSimRestart((n) => n + 1);
                  setPreviewing(true);
                }}
                title={t.t("Play the draft from the start, as a team")}
                aria-label={t.t("Test play")}
              >
                <Play size={ICON.sm} />
                <span className="toolbar-label">{t.t("Test play")}</span>
              </Button>
              <Button
                variant="ghost"
                size="icon"
                title={t.t("More")}
                aria-label={t.t("More")}
                onClick={(e) => {
                  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                  setKebab({ x: r.right - 200, y: r.bottom + 4 });
                }}
              >
                <MoreVertical size={ICON.md} />
              </Button>
            </div>
          </div>

          <TopBarPortal>
            <PresenceBar peers={presence.others} following={following} onFollow={follow} describe={describePeer} />
          </TopBarPortal>

          <div className="editor-banner-stack">
            {conflict && (
              <div className="editor-conn-banner" role="alert">
                <span className="editor-conn-banner-text">
                  {t.t("Someone else saved this adventure while you were editing, so your latest changes are not saved yet.")}
                </span>
                <Button size="sm" variant="primary" onClick={() => void pullRemote(null)}>{t.t("Load theirs")}</Button>
                <Button
                  size="sm"
                  onClick={() => {
                    since.current = undefined;
                    setConflict(false);
                  }}
                >
                  {t.t("Keep mine")}
                </Button>
              </div>
            )}
            {clash && (
              <div className="editor-conn-banner editor-conn-banner--info" role="status">
                <span className="editor-conn-banner-text">
                  {t.t("You and {{name}} both changed the same thing, so your version was kept:", { name: clash.by || t.t("someone") })}{" "}
                  {clash.steps.map((x, i) => {
                    const n = draft ? nodeBySlug(draft, x) : undefined;
                    return (
                      <span key={x}>
                        {i > 0 && ", "}
                        {n ? (
                          <button type="button" className="link-button" onClick={() => reveal(x)}>
                            {n.title || n.slug}
                          </button>
                        ) : (
                          x
                        )}
                      </span>
                    );
                  })}
                </span>
                <Button size="sm" variant="ghost" onClick={() => setClash(null)} aria-label={t.t("Dismiss")}>
                  <X size={ICON.sm} />
                </Button>
              </div>
            )}
            {saveError && (
              <div className="editor-conn-banner" role="alert">
                <span className="editor-conn-banner-text">{saveError}</span>
              </div>
            )}
            {!isLive && !preview && (
              <div className="editor-conn-banner editor-conn-banner--info" role="status">
                <Info size={ICON.md} className="callout-icon" aria-hidden="true" />
                <span className="editor-conn-banner-text">{t.t("Not published yet, so players can't play it. Publish from the toolbar when it's ready.")}</span>
              </div>
            )}
          </div>

          <div
            className={"dz-stage" + (followed ? " is-following" : "")}
            ref={stage}
            style={followed ? ({ "--peer": peerColor(followed) } as React.CSSProperties) : undefined}
          >
          {followed && (
            <div className="follow-chip" role="status">
              <span>
                {followed.kind === "assistant"
                  ? t.t("Following {{name}}'s assistant", { name: followed.name.replace(/@.*/, "") })
                  : t.t("Following {{name}}", { name: followed.name.replace(/@.*/, "") })}
              </span>
              <button type="button" onClick={stopFollowing} aria-label={t.t("Stop following")}>
                <X size={ICON.sm} />
              </button>
            </div>
          )}
          {ticker.length > 0 && !preview && (
            <ol className="live-ticker" aria-live="polite">
              {ticker.map((e) => (
                <li key={e.id} className="live-ticker__item" style={{ "--peer": e.color } as React.CSSProperties}>
                  <button type="button" disabled={!e.slug || !draft || !nodeBySlug(draft, e.slug)} onClick={() => e.slug && reveal(e.slug)}>
                    {e.assistant && <Bot size={ICON.sm} aria-hidden />}
                    <span className="live-ticker__text">{e.text}</span>
                    {e.note && <span className="live-ticker__note">{e.note}</span>}
                  </button>
                </li>
              ))}
            </ol>
          )}
          {preview && (
            <div className={"history-preview-banner" + (slot === "history" ? " with-panel" : "")}>
              <span className="history-preview-msg">
                <History className="icon-lede" size={ICON.sm} />
                {t.t("Viewing version {{n}}", { n: preview.version })}
              </span>
              <div className="history-preview-actions">
                <Button variant="primary" onClick={restore}>
                  <RotateCcw size={ICON.sm} />
                  {t.t("Restore this version")}
                </Button>
                <Button variant="ghost" onClick={() => setPreview(null)}>
                  {t.t("Back to latest")}
                </Button>
              </div>
            </div>
          )}

          {listView ? (
            <div className="dz-list">
              <ListView
                graph={graph}
                issues={shownIssues}
                selected={selected}
                playing={previewing ? playing : null}
                changed={changed}
                {...(preview ? {} : { marks })}
                onChange={change}
                onSelect={choose}
                onOpenCanvas={(slug) => {
                  setListView(false);
                  if (slug) {
                    setSelected(slug);
                    setFocusRequest((n) => n + 1);
                  }
                }}
              />
            </div>
          ) : (
            <ReactFlowProvider>
              <Canvas
                graph={graph}
                issues={preview ? [] : shownIssues}
                selected={selected}
                playing={previewing ? playing : null}
                changed={changed}
                {...(preview ? {} : { marks, live: canvasLive, ...(fx ? { fx } : {}) })}
                onChange={change}
                onSelect={(s) => {
                  choose(s);
                  if (s && slot === "history") setSlot("inspect");
                }}
                paletteRequest={paletteRequest}
                focusRequest={focusRequest}
                revealRequest={revealRequest}
              />
            </ReactFlowProvider>
          )}
          </div>
        </div>

        <div className="inspector">
          {inspectorWidth.handle}
          {slot === "history" ? (
            <HistoryPanel
              versions={versions}
              published={published}
              preview={preview?.version ?? null}
              publishing={publishing}
              onClose={() => {
                setSlot("inspect");
                setPreview(null);
              }}
              onPreview={(v) => void previewVersion(v)}
              onLabel={setLabelEditing}
              onMakeLive={(v) => (v.label ? void doPublish({ kind: "rollback", version: v.version }, "") : setPublishAsk({ kind: "rollback", version: v.version }))}
            />
          ) : node && translating ? (
            <TranslateStep node={node} graph={graph} lang={editLang} onGraphChange={change} onClose={() => choose(null)} readOnly={readOnly} />
          ) : node ? (
            <AdventureSlug.Provider value={slug}>
              {changed.has(node.slug) && (
                <div key={fxSeq} className="inspector__flash" style={{ "--changed": changed.get(node.slug) } as React.CSSProperties} aria-hidden />
              )}
              {holder && (
                <div className="inspector__held" role="status" style={{ "--peer": peerColor(holder) } as React.CSSProperties}>
                  <span>{t.t("{{name}} is editing this step. Your changes would mix with theirs.", { name: holder.name.replace(/@.*/, "") })}</span>
                  <Button size="sm" onClick={() => setTakenOver((s) => new Set(s).add(node.slug))}>
                    {t.t("Edit anyway")}
                  </Button>
                </div>
              )}
              <Inspector
                key={node.slug}
                node={node}
                graph={graph}
                readOnly={readOnly || !!holder}
                issues={shownIssues.filter((i) => i.node === node.slug)}
                onChange={(n) => change(updateNode(graph, node.slug, () => n))}
                onGraphChange={change}
                onClose={() => choose(null)}
                onPreview={() => {
                  setSimMode("step");
                  setPreviewing(true);
                }}
                onDelete={() => {
                  change(removeNode(graph, node.slug));
                  setSelected(null);
                }}
              />
            </AdventureSlug.Provider>
          ) : null}
        </div>
      </div>

      {previewing && (
        <FloatingWindow title={t.t("Preview")} storageKey="zonventure.preview.pos" onClose={() => setPreviewing(false)}>
          <Simulator
            slug={slug}
            graph={draft!}
            envelope={simEnvelope!}
            selected={selected}
            mode={simMode}
            onMode={setSimMode}
            restart={simRestart}
            onPosition={setPlaying}
            brand={brand}
            title={adventureTitle}
          />
        </FloatingWindow>
      )}

      {kebab && <CanvasContextMenu x={kebab.x} y={kebab.y} items={kebabItems} onClose={() => setKebab(null)} />}

      {publishAsk && (
        <PublishLabelModal
          title={
            publishAsk.kind === "rollback"
              ? t.t("Publish this version?")
              : publishAsk.kind === "first"
                ? t.t("Publish this adventure?")
                : t.t("Publish changes?")
          }
          message={
            publishAsk.kind === "rollback"
              ? t.t("Events that start from now on play this version. Events already running keep theirs.")
              : t.t("Events that start from now on play what you see now. Events already running keep the version they started with.")
          }
          confirmLabel={publishAsk.kind === "update" ? t.t("Publish changes") : t.t("Publish")}
          {...(errors.length && publishAsk.kind !== "rollback"
            ? { blocked: true, warning: t.plural(errors.length, { one: "{{n}} error must be fixed first — publishing will be refused.", other: "{{n}} errors must be fixed first — publishing will be refused." }) }
            : {})}
          onPublish={(label) => void doPublish(publishAsk, label)}
          onCancel={() => setPublishAsk(null)}
        />
      )}
      {diff && (
        <DiffDialog
          diff={diff.diff}
          loading={diff.loading}
          publishing={publishing}
          canPublish={!readOnly}
          onPublish={() => setPublishAsk({ kind: "update" })}
          onClose={() => setDiff(null)}
        />
      )}
      {labelEditing && (
        <PromptModal
          title={t.t("Name this version so it's easy to find later.")}
          label={t.t("Version label")}
          hint={t.t("Leave blank to clear the label.")}
          initialValue={labelEditing.label ?? ""}
          confirmLabel={t.t("Save label")}
          allowEmpty
          onSubmit={(v) => void saveLabel(labelEditing, v)}
          onCancel={() => setLabelEditing(null)}
        />
      )}
      {settingsOpen && (
        <PanelModal title={t.t("Adventure settings")} wide onClose={() => setSettingsOpen(false)}>
          <div className="dz-settings">
            <AdventureSlug.Provider value={slug}>
              <Resources graph={graph} onChange={change} />
            </AdventureSlug.Provider>
            <LanguagesSettings
              graph={graph}
              main={mainLang}
              meta={meta}
              onChange={change}
              onMainChange={async (l) => {
                await adventures.saveLanguage(slug, l);
                setAdventureLang(l);
                // Also the draft's copy, so the simulator and the next
                // save agree with the adventure straight away.
                change({ ...graph, language: l });
              }}
              editing={editLang}
              onEditing={setEditLang}
            />
            <Look
              brand={brand}
              dirty={brandDirty}
              saving={brandSaving}
              error={brandError}
              onChange={setBrand}
              onSave={async () => {
                setBrandSaving(true);
                setBrandError(null);
                try {
                  await adventures.saveBrand(slug, brand);
                  setBrandSaved(brand);
                } catch (e) {
                  setBrandError(e instanceof ApiError ? e.message : t.t("Could not reach the server. Check your connection."));
                } finally {
                  setBrandSaving(false);
                }
              }}
              onRevert={() => setBrand(brandSaved)}
            />
          </div>
        </PanelModal>
      )}
      {helpOpen && <HelpModal onClose={() => setHelpOpen(false)} />}
      {searchOpen && graph && (
        <StepSearch
          graph={graph}
          {...(stage.current ? { bounds: stage.current.getBoundingClientRect() } : {})}
          onPick={(n) => openStep(n.slug)}
          onClose={() => setSearchOpen(false)}
        />
      )}
      {celebrate && <PublishCelebration />}
    </div>
  );
}

function IssueText({ graph, issue }: { graph: Graph; issue: Issue }) {
  return (
    <>
      {issue.node && (
        <strong>
          {nodeBySlug(graph, issue.node)?.title || issue.node}
          {issue.port ? ` · ${portLabelFor(graph, issue.node, issue.port)}` : ""}:{" "}
        </strong>
      )}
      {issue.message}
    </>
  );
}

/** Version history (dazyflow's .history-panel), in the right slot. */
function HistoryPanel({
  versions,
  published,
  preview,
  publishing,
  onClose,
  onPreview,
  onLabel,
  onMakeLive,
}: {
  versions: adventures.GraphVersion[] | null;
  published: Published;
  preview: number | null;
  publishing: boolean;
  onClose: () => void;
  onPreview: (v: number) => void;
  onLabel: (v: adventures.GraphVersion) => void;
  onMakeLive: (v: adventures.GraphVersion) => void;
}) {
  const t = useT();
  const when = (iso: string) => new Date(iso).toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
  return (
    <div className="history-panel dz-history-in-slot">
      <div className="history-panel-head">
        <strong>{t.t("Version history")}</strong>
        <Button size="icon" onClick={onClose} aria-label={t.t("Dismiss")}>
          <X size={ICON.md} />
        </Button>
      </div>
      {versions === null ? (
        <Notice>{t.t("Loading…")}</Notice>
      ) : versions.length === 0 ? (
        <Notice>{t.t("No published versions yet.")}</Notice>
      ) : (
        <ul className="history-list">
          {versions.map((v, i) => (
            <li key={v.version}>
              <Button className={`history-row${preview === v.version ? " active" : ""}`} onClick={() => onPreview(v.version)} title={when(v.created_at)}>
                <span className="history-row-when">
                  {i === 0 ? t.t("Newest version") : when(v.created_at)} · v{v.version}
                </span>
                <span className="history-row-meta">
                  {v.author && <span className="history-row-author">{v.author}</span>}
                  {v.label && (
                    <span className="history-badge label" title={t.t("This version's label.")}>
                      <Tag className="icon-lede" size={ICON.xs} />
                      {v.label}
                    </span>
                  )}
                  {published?.version === v.version && (
                    <span className="history-badge live" title={t.t("The live version — the one new events play.")}>
                      <Rocket className="icon-lede" size={ICON.xs} />
                      {t.t("Live version")}
                    </span>
                  )}
                </span>
              </Button>
              <Button variant="ghost" size="sm" className="history-label-btn" onClick={() => onLabel(v)} title={t.t("Name this version so it's easy to find later.")}>
                <Tag size={ICON.xs} />
                {v.label ? t.t("Rename label") : t.t("Label")}
              </Button>
              {published?.version !== v.version && (
                <Button className="history-makelive" onClick={() => onMakeLive(v)} disabled={publishing} title={t.t("Make this the live version — new events play it from now on.")}>
                  <Rocket size={ICON.xs} />
                  {t.t("Publish this version")}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
