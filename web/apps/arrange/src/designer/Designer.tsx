import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { ApiError, adventures } from "@zonventure/api";
import {
  FORMAT_V2,
  canRedo,
  canUndo,
  cleanGraph,
  differs,
  emptyGraph,
  linearSteps,
  nodeBySlug,
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
  type HistoryState,
  type Issue,
  type Report,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { href } from "../router.ts";
import { Canvas } from "./Canvas.tsx";
import { Inspector } from "./Inspector.tsx";
import { ListView } from "./ListView.tsx";
import { Resources } from "./Resources.tsx";
import "./designer.css";

type Loaded =
  | { state: "loading" }
  | { state: "error"; message: string }
  | { state: "none" } // adventure has no graph yet
  | { state: "ready" };

const envelope = (g: Graph): Envelope => ({ format: FORMAT_V2, graph: cleanGraph(g) });

/**
 * The adventure designer: canvas or list, inspector, validation, save.
 *
 * The graph document is the single source of truth, held in an undo
 * history of whole snapshots (history.ts, adapted from dazyflow). The
 * server validates on every change — debounced — so problems appear on
 * the canvas as the author works, and a save is refused with the full
 * report if anything blocks it.
 */
export function Designer({ slug }: { slug: string }) {
  const t = useT();
  const [loaded, setLoaded] = useState<Loaded>({ state: "loading" });
  const [history, setHistory] = useState<HistoryState>(emptyHistory);
  const [saved, setSaved] = useState<Graph | null>(null);
  const [version, setVersion] = useState<number | null>(null);
  const [issues, setIssues] = useState<Issue[]>([]);
  const [validating, setValidating] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [panel, setPanel] = useState<"step" | "resources" | "issues">("step");
  const [forceCanvas, setForceCanvas] = useState(false);
  const [paletteRequest, setPaletteRequest] = useState(0);
  const [focusRequest, setFocusRequest] = useState(0);

  const graph = history.present;

  // --- load ----------------------------------------------------------------

  const adopt = useCallback((g: Graph, v: number | null, report: Report | null) => {
    setHistory(rebase(g, Date.now()));
    setSaved(v === null ? null : g);
    setVersion(v);
    setIssues(report?.issues ?? []);
    setLoaded({ state: "ready" });
  }, []);

  useEffect(() => {
    const ac = new AbortController();
    adventures
      .getGraph<Envelope, Report>(slug, ac.signal)
      .then((r) => {
        if (ac.signal.aborted) return;
        if (!r) setLoaded({ state: "none" });
        else adopt(r.envelope.graph, r.version ?? null, r.report);
      })
      .catch((e: unknown) => {
        if (!ac.signal.aborted) setLoaded({ state: "error", message: e instanceof Error ? e.message : String(e) });
      });
    return () => ac.abort();
  }, [slug, adopt]);

  const startFromLanes = async () => {
    setLoaded({ state: "loading" });
    try {
      const r = await adventures.convertGraph<Envelope, Report>(slug);
      adopt(tidy(r.envelope.graph), null, r.report);
    } catch (e) {
      setLoaded({ state: "error", message: e instanceof Error ? e.message : String(e) });
    }
  };

  // --- editing ----------------------------------------------------------------

  const change = useCallback((g: Graph) => {
    setHistory((h) => record(h, g, Date.now()));
    setSaveError(null);
  }, []);

  const doUndo = useCallback(() => {
    setHistory((h) => undo(h)?.state ?? h);
  }, []);
  const doRedo = useCallback(() => {
    setHistory((h) => redo(h)?.state ?? h);
  }, []);

  // --- validation, debounced ----------------------------------------------------

  useEffect(() => {
    if (!graph) return;
    const ac = new AbortController();
    const timer = setTimeout(() => {
      setValidating(true);
      adventures
        .validateGraph<Envelope, Report>(slug, envelope(graph), ac.signal)
        .then((r) => setIssues(r.report.issues ?? []))
        .catch(() => {
          /* network blip: keep the last report rather than flash "all clear" */
        })
        .finally(() => {
          if (!ac.signal.aborted) setValidating(false);
        });
    }, 400);
    return () => {
      clearTimeout(timer);
      ac.abort();
    };
  }, [graph, slug]);

  // --- save ---------------------------------------------------------------------

  const dirty = !!graph && (saved === null || differs(graph, saved));
  const errors = issues.filter((i) => i.severity === "error");
  const warnings = issues.filter((i) => i.severity === "warning");

  const save = useCallback(async () => {
    if (!graph || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      const r = await adventures.saveGraph<Envelope, Report>(slug, envelope(graph));
      setSaved(graph);
      setVersion(r.version ?? null);
      setIssues(r.report.issues ?? []);
    } catch (e) {
      const report = adventures.refusedReport<Report>(e);
      if (report) {
        setIssues(report.issues ?? []);
        setSaveError(t.t("Not saved: fix the errors marked in red first."));
        setPanel("issues");
      } else {
        setSaveError(e instanceof ApiError || e instanceof Error ? e.message : String(e));
      }
    } finally {
      setSaving(false);
    }
  }, [graph, saving, slug, t]);

  // Warn before leaving with unsaved work — navigating away from a canvas
  // you spent an hour on should never be silent.
  useEffect(() => {
    if (!dirty) return;
    const onBefore = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onBefore);
    return () => window.removeEventListener("beforeunload", onBefore);
  }, [dirty]);

  // --- keyboard ----------------------------------------------------------------

  const saveRef = useRef(save);
  saveRef.current = save;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      const inField = (e.target as HTMLElement)?.closest?.("input, textarea, select");
      const k = e.key.toLowerCase();
      if (k === "s") {
        e.preventDefault();
        void saveRef.current();
      } else if (k === "k") {
        e.preventDefault();
        setForceCanvas(true);
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
  }, [doUndo, doRedo]);

  // --- derived --------------------------------------------------------------------

  const steps = useMemo(() => (graph ? linearSteps(graph) : null), [graph]);
  const showList = !!steps && !forceCanvas;
  const node = graph && selected ? nodeBySlug(graph, selected) : undefined;

  useEffect(() => {
    if (selected) setPanel("step");
  }, [selected]);

  // --- render ---------------------------------------------------------------------

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
        <a className="link-btn" href={href({ name: "adventures" })}>← {t.t("Adventures")}</a>
        <h1>{t.t("Design this adventure")}</h1>
        <p className="lede">
          {t.t("This adventure has no flow yet. Start from its existing puzzles, or from a blank canvas.")}
        </p>
        <div className="designer-empty__actions">
          <button type="button" className="btn btn--primary" onClick={() => void startFromLanes()}>
            {t.t("Start from existing puzzles")}
          </button>
          <button type="button" className="btn btn--ghost" onClick={() => adopt(emptyGraph(), null, null)}>
            {t.t("Start blank")}
          </button>
        </div>
        <p className="field__hint">{t.t("Nothing is saved until you press Save.")}</p>
      </div>
    );
  if (!graph) return null;

  return (
    <div className="designer">
      <header className="designer__bar">
        <a className="link-btn" href={href({ name: "adventures" })}>← {t.t("Adventures")}</a>
        <strong className="designer__title">{slug}</strong>

        <div className="seg" role="group" aria-label={t.t("View")}>
          <button type="button" className={showList ? "is-on" : ""} disabled={!steps} onClick={() => setForceCanvas(false)}
            title={!steps ? t.t("The list shows a straight line of puzzles and stories. This adventure needs the canvas.") : undefined}>
            {t.t("List")}
          </button>
          <button type="button" className={!showList ? "is-on" : ""} onClick={() => setForceCanvas(true)}>
            {t.t("Canvas")}
          </button>
        </div>

        <span className="designer__spacer" />

        <button type="button" className="icon-btn" onClick={doUndo} disabled={!canUndo(history)} aria-label={t.t("Undo")} title={t.t("Undo (Ctrl+Z)")}>↶</button>
        <button type="button" className="icon-btn" onClick={doRedo} disabled={!canRedo(history)} aria-label={t.t("Redo")} title={t.t("Redo (Ctrl+Shift+Z)")}>↷</button>
        {!showList && (
          <>
            <button type="button" className="btn btn--ghost btn--sm" onClick={() => setPaletteRequest((n) => n + 1)} title="Ctrl+K">
              + {t.t("Add")}
            </button>
            <button type="button" className="btn btn--ghost btn--sm" onClick={() => change(tidy(graph))}>
              {t.t("Tidy")}
            </button>
          </>
        )}

        <button type="button" className={"status" + (errors.length ? " status--error" : warnings.length ? " status--warning" : " status--ok")}
          onClick={() => setPanel("issues")}>
          {validating ? t.t("Checking…") : errors.length ? t.plural(errors.length, { one: "{{n}} error", other: "{{n}} errors" })
            : warnings.length ? t.plural(warnings.length, { one: "{{n}} warning", other: "{{n}} warnings" }) : t.t("Ready to play")}
        </button>

        <span className="designer__version">
          {version ? t.t("v{{n}}", { n: version }) : t.t("unsaved")}
          {dirty ? " •" : ""}
        </span>
        <button type="button" className="btn btn--primary btn--sm" onClick={() => void save()} disabled={saving || !dirty}>
          {saving ? t.t("Saving…") : t.t("Save")}
        </button>
      </header>

      {saveError && <div className="alert alert--danger designer__alert" role="alert">{saveError}</div>}

      <div className="designer__body">
        <div className="designer__main">
          {showList ? (
            <ListView graph={graph} steps={steps!} issues={issues} selected={selected} onChange={change}
              onSelect={setSelected} onOpenCanvas={() => setForceCanvas(true)} />
          ) : (
            <ReactFlowProvider>
              <Canvas graph={graph} issues={issues} selected={selected} onChange={change} onSelect={setSelected}
                paletteRequest={paletteRequest} focusRequest={focusRequest} />
            </ReactFlowProvider>
          )}
        </div>

        <aside className="designer__side">
          <div className="tabs" role="tablist">
            <button type="button" role="tab" aria-selected={panel === "step"} className={panel === "step" ? "is-on" : ""} onClick={() => setPanel("step")}>
              {t.t("Step")}
            </button>
            <button type="button" role="tab" aria-selected={panel === "resources"} className={panel === "resources" ? "is-on" : ""} onClick={() => setPanel("resources")}>
              {t.t("Zones & items")}
            </button>
            <button type="button" role="tab" aria-selected={panel === "issues"} className={panel === "issues" ? "is-on" : ""} onClick={() => setPanel("issues")}>
              {t.t("Problems")} {issues.length > 0 && <span className="tabs__count">{issues.length}</span>}
            </button>
          </div>

          {panel === "step" &&
            (node ? (
              <Inspector
                key={node.slug}
                node={node}
                graph={graph}
                issues={issues.filter((i) => i.node === node.slug)}
                onChange={(n) => change(updateNode(graph, node.slug, () => n))}
                onDelete={() => {
                  change(removeNode(graph, node.slug));
                  setSelected(null);
                }}
              />
            ) : (
              <div className="insp insp--empty">
                <p>{t.t("Select a step to edit it.")}</p>
                {!showList && (
                  <p className="field__hint">
                    {t.t("Drag from any output and let go on empty space to add what comes next. Double-click the canvas or press Ctrl+K to add a step anywhere.")}
                  </p>
                )}
              </div>
            ))}

          {panel === "resources" && <Resources graph={graph} onChange={change} />}

          {panel === "issues" && (
            <div className="insp">
              {issues.length === 0 ? (
                <p className="lede">{t.t("No problems. Every step is reachable and every team can finish.")}</p>
              ) : (
                <ul className="issues">
                  {[...errors, ...warnings].map((i, k) => (
                    <li key={k}>
                      <button
                        type="button"
                        className={`issue issue--${i.severity}`}
                        disabled={!i.node}
                        onClick={() => {
                          if (!i.node) return;
                          setSelected(i.node);
                          setForceCanvas(true);
                          setFocusRequest((n) => n + 1);
                        }}
                      >
                        {i.node && <strong>{nodeBySlug(graph, i.node)?.title || i.node}{i.port ? ` · ${i.port}` : ""}: </strong>}
                        {i.message}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
