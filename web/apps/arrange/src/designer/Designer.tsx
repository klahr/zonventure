import { useCallback, useEffect, useMemo, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import {
  ArrowLeft,
  Check,
  GitCompare,
  History,
  LayoutGrid,
  ListOrdered,
  Loader2,
  MoreVertical,
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
  diffGraphs,
  differs,
  emptyGraph,
  linearSteps,
  nodeBySlug,
  portLabel,
  rebase,
  record,
  redo,
  removeNode,
  tidy,
  undo,
  updateNode,
  emptyHistory,
  type Envelope,
  type Graph,
  type GraphDiff,
  type HistoryState,
  type Issue,
  type Report,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { href } from "../router.ts";
import { Canvas, cardSizes } from "./Canvas.tsx";
import { Inspector } from "./Inspector.tsx";
import { ListView } from "./ListView.tsx";
import { Resources } from "./Resources.tsx";
import { Simulator, type SimMode } from "./Simulator.tsx";
import { FloatingWindow } from "./FloatingWindow.tsx";
import { Look } from "./Look.tsx";
import { AdventureSlug } from "./MediaEditor.tsx";
import { CanvasContextMenu, type ContextMenuItem } from "./dz/CanvasContextMenu.tsx";
import { DiffDialog, HelpModal, PanelModal, PublishCelebration, PublishLabelModal } from "./dz/dialogs.tsx";
import { Button, ICON, IssueRow, IssuesButton, Notice, PromptModal } from "./dz/ui.tsx";
import "./dz.css";
import "./designer.css";

type Loaded =
  | { state: "loading" }
  | { state: "error"; message: string }
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
  const [kebab, setKebab] = useState<{ x: number; y: number } | null>(null);

  const [simMode, setSimMode] = useState<SimMode>("step");
  const [simRestart, setSimRestart] = useState(0);
  const [playing, setPlaying] = useState<string | null>(null);
  // The preview floats beside the inspector, so a step can be edited and
  // watched at once.
  const [previewing, setPreviewing] = useState(false);
  const [listView, setListView] = useState(false);
  const [paletteRequest, setPaletteRequest] = useState(0);
  const [focusRequest, setFocusRequest] = useState(0);

  // The adventure's palette, edited in Adventure settings.
  const [brandSaved, setBrandSaved] = useState<play.Brand>({});
  const [brand, setBrand] = useState<play.Brand>({});
  const [brandSaving, setBrandSaving] = useState(false);
  const [brandError, setBrandError] = useState<string | null>(null);
  const [adventureTitle, setAdventureTitle] = useState(slug);
  const brandDirty = JSON.stringify(brand) !== JSON.stringify(brandSaved);

  const draft = history.present;
  // What the canvas shows: an older version while previewing, else the draft.
  const graph = preview?.graph ?? draft;
  const readOnly = !!preview;

  // --- load -------------------------------------------------------------------

  const adopt = useCallback((r: adventures.GraphPayload<Envelope, Report> | null, g: Graph, savedGraph: Graph | null) => {
    setHistory(rebase(g, Date.now()));
    setSaved(savedGraph);
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
        if (!ac.signal.aborted) setLoaded({ state: "error", message: e instanceof Error ? e.message : String(e) });
      });
    adventures
      .getLook(slug, ac.signal)
      .then(({ title, brand: b }) => {
        if (title) setAdventureTitle(title);
        setBrandSaved(b);
        setBrand(b);
      })
      .catch(() => {
        /* no palette is the default look */
      });
    return () => ac.abort();
  }, [slug, adopt]);

  const startFromLanes = async () => {
    setLoaded({ state: "loading" });
    try {
      const r = await adventures.convertGraph<Envelope, Report>(slug);
      adopt(r, tidy(r.envelope.graph), null);
    } catch (e) {
      setLoaded({ state: "error", message: e instanceof Error ? e.message : String(e) });
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
  const doUndo = useCallback(() => setHistory((h) => undo(h)?.state ?? h), []);
  const doRedo = useCallback(() => setHistory((h) => redo(h)?.state ?? h), []);

  // --- autosave ------------------------------------------------------------------

  const unsaved = !!draft && (saved === null || differs(draft, saved));
  const saveNow = useCallback(async (): Promise<boolean> => {
    if (!draft) return false;
    setSaving(true);
    try {
      const r = await adventures.saveGraph<Envelope, Report>(slug, envelope(draft));
      setSaved(draft);
      setIssues(r.report.issues ?? []);
      setPublished(r.published ?? null);
      setServerDirty(!!r.dirty);
      setSaveError(null);
      return true;
    } catch (e) {
      setSaveError(e instanceof ApiError || e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setSaving(false);
    }
  }, [draft, slug]);

  useEffect(() => {
    if (!unsaved || readOnly || loaded.state !== "ready") return;
    const id = setTimeout(() => void saveNow(), AUTOSAVE_MS);
    return () => clearTimeout(id);
  }, [unsaved, readOnly, loaded.state, saveNow]);

  // Validation between autosaves, so badges follow the author quickly.
  useEffect(() => {
    if (!draft || readOnly) return;
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
  }, [draft, readOnly, slug]);

  useEffect(() => {
    if (!unsaved) return;
    const onBefore = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onBefore);
    return () => window.removeEventListener("beforeunload", onBefore);
  }, [unsaved]);

  // --- publish ----------------------------------------------------------------------

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
        setSaveError(e instanceof Error ? e.message : String(e));
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
      setSaveError(e instanceof Error ? e.message : String(e));
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
      setSaveError(e instanceof Error ? e.message : String(e));
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
  }, [doUndo, doRedo, saveNow]);

  // --- derived ----------------------------------------------------------------------

  const steps = useMemo(() => (graph ? linearSteps(graph) : null), [graph]);
  const showList = listView && !!steps;
  const node = graph && selected ? nodeBySlug(graph, selected) : undefined;
  const simEnvelope = useMemo(() => (draft ? envelope(draft) : null), [draft]);
  const slotOpen = slot !== "inspect" || !!node;

  const jumpTo = (i: Issue) => {
    if (!i.node) return;
    setIssuePanel(null);
    setSelected(i.node);
    setSlot("inspect");
    setListView(false);
    setFocusRequest((n) => n + 1);
  };

  // --- render ----------------------------------------------------------------------

  if (loaded.state === "loading") return <p className="lede">{t.t("Loading…")}</p>;
  if (loaded.state === "error")
    return (
      <div className="alert alert--danger" role="alert">
        {loaded.message}
      </div>
    );
  if (loaded.state === "none")
    return (
      <div className="designer-empty">
        <a className="link-btn" href={href({ name: "adventures" })}>
          ← {t.t("Adventures")}
        </a>
        <h1>{t.t("Design this adventure")}</h1>
        <p className="lede">{t.t("This adventure has no flow yet. Start from its existing puzzles, or from a blank canvas.")}</p>
        <div className="designer-empty__actions">
          <button type="button" className="btn btn--primary" onClick={() => void startFromLanes()}>
            {t.t("Start from existing puzzles")}
          </button>
          <button type="button" className="btn btn--ghost" onClick={() => adopt(null, emptyGraph(), null)}>
            {t.t("Start blank")}
          </button>
        </div>
      </div>
    );
  if (!graph) return null;

  const isLive = published !== null;
  const kebabItems: ContextMenuItem[] = [
    { label: t.t("Adventure settings"), onClick: () => setSettingsOpen(true) },
    { label: t.t("Keyboard shortcuts"), shortcut: "?", onClick: () => setHelpOpen(true) },
  ];

  return (
    <div className="dz dz-designer">
      <div className="editor" data-has-selection={slotOpen ? "true" : "false"}>
        <div className="canvas">
          <div className="editor-toolbar">
            <a className="dz-back" href={href({ name: "adventures" })} title={t.t("Adventures")} aria-label={t.t("Adventures")}>
              <ArrowLeft size={ICON.md} />
            </a>
            <strong className="dz-title" title={adventureTitle}>
              {adventureTitle}
            </strong>
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
                <Button
                  variant="ghost"
                  className="editor-add-drop"
                  disabled={readOnly}
                  onClick={() => {
                    setListView(false);
                    setPaletteRequest((n) => n + 1);
                  }}
                  title={t.t("Search and insert a step (Ctrl/Cmd+K)")}
                  aria-label={t.t("Add step")}
                >
                  <Plus size={ICON.sm} />
                  <span className="toolbar-label">{t.t("Add step")}</span>
                </Button>
                <Button
                  variant="ghost"
                  className={"editor-dataview" + (showList ? " on" : "")}
                  aria-pressed={showList}
                  disabled={!steps}
                  onClick={() => setListView((v) => !v)}
                  title={steps ? t.t("Show the adventure as a list of steps") : t.t("The list shows a straight line of puzzles and stories. This adventure needs the canvas.")}
                  aria-label={t.t("List view")}
                >
                  <ListOrdered size={ICON.sm} />
                  <span className="toolbar-label">{t.t("List view")}</span>
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

            <Button variant="ghost" data-tidy="toolbar" disabled={readOnly} onClick={() => change(tidy(graph, cardSizes()))} title={t.t("Auto-arrange the adventure into clean columns")}>
              <LayoutGrid size={ICON.sm} aria-hidden="true" />
              <span className="toolbar-label">{t.t("Tidy")}</span>
            </Button>

            <IssuesButton
              kind="error"
              count={errors.length}
              title={t.plural(errors.length, { one: "{{n}} error", other: "{{n}} errors" })}
              heading={t.plural(errors.length, { one: "{{n}} error to fix before publishing", other: "{{n}} errors to fix before publishing" })}
              open={issuePanel === "error"}
              onOpenChange={(o) => setIssuePanel(o ? "error" : null)}
            >
              {errors.map((i, k) => (
                <IssueRow key={k} text={<IssueText graph={graph} issue={i} />} actions={i.node ? <Button variant="link" onClick={() => jumpTo(i)}>{t.t("Show")}</Button> : undefined} />
              ))}
            </IssuesButton>
            <IssuesButton
              kind="warning"
              count={warnings.length}
              title={t.plural(warnings.length, { one: "{{n}} warning", other: "{{n}} warnings" })}
              heading={t.plural(warnings.length, { one: "{{n}} warning", other: "{{n}} warnings" })}
              open={issuePanel === "warning"}
              onOpenChange={(o) => setIssuePanel(o ? "warning" : null)}
            >
              {warnings.map((i, k) => (
                <IssueRow key={k} text={<IssueText graph={graph} issue={i} />} actions={i.node ? <Button variant="link" onClick={() => jumpTo(i)}>{t.t("Show")}</Button> : undefined} />
              ))}
            </IssuesButton>

            <div className="editor-publish-group">
              <Button
                role="switch"
                aria-checked={isLive}
                className={"editor-publish-toggle" + (isLive ? " on" : "") + (celebrate ? " celebrate" : "")}
                onClick={() => (isLive ? dirty && setPublishAsk({ kind: "update" }) : setPublishAsk({ kind: "first" }))}
                disabled={publishing || readOnly}
                title={
                  readOnly
                    ? t.t("Exit version preview before publishing.")
                    : isLive
                      ? t.t("Published: events that start now play version {{n}}.", { n: published.version })
                      : t.t("Publish this adventure so events can play it.")
                }
              >
                <span className="editor-publish-track" aria-hidden="true">
                  <span className="editor-publish-knob">
                    <Rocket size={ICON.xs} strokeWidth={2.4} />
                  </span>
                </span>
                <span className="toolbar-label">{publishing ? t.t("Publishing…") : isLive ? t.t("Live") : t.t("Publish")}</span>
              </Button>
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

          <div className="editor-banner-stack">
            {saveError && (
              <div className="editor-conn-banner" role="alert">
                <span className="editor-conn-banner-text">{saveError}</span>
              </div>
            )}
            {!isLive && !preview && (
              <div className="editor-conn-banner" role="status">
                <span className="editor-conn-banner-text">{t.t("This adventure hasn't been published yet, so events can't play it.")}</span>
                <span className="editor-conn-banner-actions">
                  <Button variant="primary" onClick={() => setPublishAsk({ kind: "first" })} disabled={publishing}>
                    {t.t("Publish")}
                  </Button>
                </span>
              </div>
            )}
            {isLive && !preview && (
              <div className="editor-live-state" role="status">
                {dirty ? (
                  <>
                    <span className="editor-live-dot dirty" aria-hidden="true" />
                    {t.t("Published, but your draft has changes that aren't live yet.")}
                    <Button variant="link" className="editor-live-action" onClick={() => setPublishAsk({ kind: "update" })} disabled={publishing}>
                      {t.t("Publish changes")}
                    </Button>
                  </>
                ) : (
                  <>
                    <span className="editor-live-dot" aria-hidden="true" />
                    {t.t("Published — the live version matches your draft.")}
                  </>
                )}
              </div>
            )}
          </div>

          <div className="dz-stage">
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

          {showList ? (
            <div className="dz-list">
              <ListView
                graph={graph}
                steps={steps!}
                issues={issues}
                selected={selected}
                playing={previewing ? playing : null}
                onChange={change}
                onSelect={setSelected}
                onOpenCanvas={() => setListView(false)}
              />
            </div>
          ) : (
            <ReactFlowProvider>
              <Canvas
                graph={graph}
                issues={preview ? [] : issues}
                selected={selected}
                playing={previewing ? playing : null}
                onChange={change}
                onSelect={(s) => {
                  setSelected(s);
                  if (s && slot === "history") setSlot("inspect");
                }}
                paletteRequest={paletteRequest}
                focusRequest={focusRequest}
              />
            </ReactFlowProvider>
          )}
          </div>
        </div>

        <div className="inspector">
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
          ) : node ? (
            <AdventureSlug.Provider value={slug}>
              <Inspector
                key={node.slug}
                node={node}
                graph={graph}
                readOnly={readOnly}
                issues={issues.filter((i) => i.node === node.slug)}
                onChange={(n) => change(updateNode(graph, node.slug, () => n))}
                onGraphChange={change}
                onClose={() => setSelected(null)}
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
            ? { warning: t.plural(errors.length, { one: "{{n}} error must be fixed first — publishing will be refused.", other: "{{n}} errors must be fixed first — publishing will be refused." }) }
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
            <Resources graph={graph} onChange={change} />
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
