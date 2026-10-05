import { useContext, useEffect, useState, type ReactNode } from "react";
import type {
  Branch,
  EffectConfig,
  Graph,
  Hint,
  HubConfig,
  Issue,
  Matcher,
  MatcherKind,
  Node,
  Output,
  Policy,
  PuzzleConfig,
  Case,
  Response,
  SwitchConfig,
  AnswerBar,
  AnswerKind,
  AnswerOf,
  ChoiceField,
  LockSymbol,
  ItemSlot,
  MissingHint,
  Trigger,
  WebhookMethod,
  Zone,
} from "@zonventure/graph";
import { ANSWER_KINDS, MAX_SLOTS, usesMatchers, WEBHOOK_METHODS, answerOptions, isGraded, optionValue, withAnswerKind, withBlocks, matcherLabel, nodeBySlug, portLabel, puzzlesInto, referAnswer, resolveAnswer, updateNode } from "@zonventure/graph";
import { THEMES } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { emails, siteName } from "@zonventure/api";
import { useSession } from "../session.tsx";
import { Dialog } from "../ui/dialogs.tsx";
import { EmailPreview } from "../ui/emailPreview.tsx";
import { ConditionEditor } from "./ConditionEditor.tsx";
import { MediaEditor } from "./MediaEditor.tsx";
import { OpenThemes } from "./ThemesModal.tsx";
import { BlocksEditor } from "./BlocksEditor.tsx";
import { Field, ZoneSelect, type ZoneHere } from "./fields.tsx";
import { AlertTriangle, ChevronLeft, ChevronRight, Eye, Plus, Trash2, X } from "lucide-react";
import { Button, HelpPopover, ICON } from "./dz/ui.tsx";
import { STEP, VISIBLE } from "./dz/types.tsx";
import { ConceptIcon, PART, TRIGGER } from "./dz/concepts.tsx";
import { ZoneMap } from "./ZoneMap.tsx";
import { nearLastZone, zoneGroups, zoneName } from "./zones.ts";
import { MarkdownArea } from "./MarkdownArea.tsx";
import { SimpleStep } from "./SimpleStep.tsx";
import { isNarrow } from "./device.ts";

interface Props {
  node: Node;
  graph: Graph;
  issues: Issue[];
  onChange: (n: Node) => void;
  /** For edits that reach beyond this node, such as naming an answer a crossroads refers to. */
  onGraphChange: (g: Graph) => void;
  onDelete: () => void;
  /** Close the inspector (deselect). */
  onClose: () => void;
  /** Open this step in the phone preview. */
  onPreview: () => void;
  /** Viewing an old version: everything shows, nothing edits. */
  readOnly?: boolean;
  /** Under the settings, never read-only: the step's work and discussion. */
  after?: ReactNode;
}

/**
 * The right-hand panel: every setting of the selected node.
 *
 * Every field writes straight through to the document; the undo history
 * coalesces consecutive edits to one node, so typing a prompt is one undo
 * step rather than one per keystroke.
 *
 * The settings come in parts (see partsOf). The overview shows each part
 * as a card: what the team reads, and a step's only setting, are edited
 * in the card; everything else shows a read-only digest with Edit, which
 * opens the part alone on a page of its own.
 */
export function Inspector({ node, graph, issues, onChange, onGraphChange, onDelete, onClose, onPreview, readOnly, after }: Props) {
  const t = useT();
  const kind = STEP[node.type];
  const Icon = kind.icon;
  const [open, setOpen] = useState<string | null>(null);
  // On a phone a puzzle or story opens on its simple form; More options
  // shows every setting, for this step until another is selected.
  const [fullFor, setFullFor] = useState<string | null>(null);
  const simple = !readOnly && isNarrow() && (node.type === "puzzle" || node.type === "story") && fullFor !== node.slug;
  const parts = partsOf(node, graph, onChange, onGraphChange, t, setOpen);
  const page = parts.find((p) => p.id === open);
  // Issues about a route, branch or path belong to the part listing them.
  const portIssues = issues.filter((i) => i.port).length;

  return (
    <>
      {/* dazyflow's inspector head: the step's icon, its name edited in
          place as the title, what the step type does, and close. */}
      <div className="panel-head inspector-head">
        <span className="inspector-identity">
          <span className={`inspector-drop-icon dz-step-icon dz-step-icon--${node.type}`} aria-hidden="true">
            <Icon size={ICON.lg} />
          </span>
          <span className="inspector-identity-text">
            <input
              className="inspector-name"
              value={node.title ?? ""}
              placeholder={t.t(kind.label)}
              spellCheck={false}
              disabled={readOnly}
              onChange={(e) => onChange({ ...node, title: e.target.value })}
              aria-label={t.t("Title")}
            />
            <span className="inspector-subtitle">{t.t(kind.label)}</span>
          </span>
          <HelpPopover label={t.t("What this step does")} body={t.t(kind.about)} />
        </span>
        <span className="inspector-head-right">
          {VISIBLE.has(node.type) && (
            <Button variant="ghost" size="icon" onClick={onPreview} aria-label={t.t("Preview this step")} title={t.t("Preview this step")}>
              <Eye size={ICON.md} />
            </Button>
          )}
          <Button variant="ghost" size="icon" className="inspector-close" onClick={onClose} aria-label={t.t("Close inspector")} title={t.t("Close inspector")}>
            <X size={ICON.md} />
          </Button>
        </span>
      </div>
      {page && (
        <div className="ipage__bar">
          <Button variant="ghost" size="sm" onClick={() => setOpen(null)} aria-label={t.t("Back to all settings")}>
            <ChevronLeft size={ICON.sm} />
            {t.t("All settings")}
          </Button>
          <span className="ipage__title">
            {PART[page.id] && <ConceptIcon {...PART[page.id]!} />}
            {page.title}
          </span>
        </div>
      )}
    <div className={"inspector-body" + (page && PART[page.id] ? ` concept--${PART[page.id]!.concept}` : "")} key={page?.id ?? ""}>
    <div className="insp inspector-edits" {...(readOnly ? { inert: true } : {})}>

      {!simple && !page && fullFor === node.slug && isNarrow() && (
        <Button variant="ghost" size="sm" className="insp__simple" onClick={() => setFullFor(null)}>
          <ChevronLeft size={ICON.sm} />
          {t.t("Back to the simple view")}
        </Button>
      )}
      {issues.length > 0 && !page && (
        <ul className="insp__issues">
          {issues.map((i, k) => (
            <li key={k} className={`issue issue--${i.severity}`}>
              {i.port ? <strong>{portLabel(node, i.port)}: </strong> : null}
              {i.message}
            </li>
          ))}
        </ul>
      )}

      {simple ? (
        <SimpleStep node={node} graph={graph} onChange={onChange} onGraphChange={onGraphChange} onDelete={onDelete} onMore={() => setFullFor(node.slug)} />
      ) : page ? (
        <>
          {page.hint && <p className="field__hint ipage__hint">{page.hint}</p>}
          <div className="ipage__body">{page.editor}</div>
        </>
      ) : (
        parts.map((p) => <PartCard key={p.id} part={p} issues={p.ports ? portIssues : 0} readOnly={!!readOnly} onOpen={() => setOpen(p.id)} />)
      )}

      {!readOnly && !page && !simple && (
        <div className="inspector-actions">
          {(node.type !== "start" || graph.nodes.filter((n) => n.type === "start").length > 1) && (
            <Button variant="danger" onClick={onDelete}>
              <Trash2 size={ICON.sm} />
              {t.t("Delete step")}
            </Button>
          )}
        </div>
      )}
    </div>
    {!page && after}
    </div>
    </>
  );
}

type T = ReturnType<typeof useT>;

/**
 * One part of a step's settings. With a summary it is a card showing that
 * digest and opening `editor` on a page of its own; without, the editor
 * sits in the card itself.
 */
interface Part {
  id: string;
  title: string;
  hint?: string | undefined;
  /** How many items the part holds, shown beside its title. */
  count?: number | undefined;
  summary?: ReactNode;
  editor: ReactNode;
  /** It lists the step's routes, branches or paths — where port issues go. */
  ports?: boolean;
}

function PartCard({ part, issues, readOnly, onOpen }: { part: Part; issues: number; readOnly: boolean; onOpen: () => void }) {
  const t = useT();
  const drill = part.summary !== undefined;
  const concept = PART[part.id];
  return (
    <section className={"ipart" + (concept ? ` concept--${concept.concept}` : "")}>
      <header className="ipart__head">
        <h3 className="ipart__title">
          {concept && <ConceptIcon {...concept} />}
          {part.title}
          {part.count !== undefined && <span className="ipart__count">{part.count}</span>}
        </h3>
        {issues > 0 && (
          <span className="ipart__issues" title={t.t("{{n}} problems here", { n: issues })}>
            <AlertTriangle size={ICON.xs} aria-hidden="true" />
            {issues}
          </span>
        )}
        {drill && <ChevronRight className="ipart__chevron" size={ICON.md} aria-hidden="true" />}
      </header>
      {drill ? (
        // The whole card opens the part: the digest's button is stretched
        // over it (see .ipart__summary::after), the chevron says so.
        <button
          type="button"
          className="ipart__summary"
          onClick={onOpen}
          aria-label={readOnly ? t.t("View {{part}}", { part: part.title }) : t.t("Edit {{part}}", { part: part.title })}
        >
          {part.summary}
        </button>
      ) : (
        <div className="ipart__body">{part.editor}</div>
      )}
    </section>
  );
}

/** A digest line: a label, and what it is set to. Empty values say so. */
function SumRow({ label, children }: { label?: ReactNode; children: ReactNode }) {
  return (
    <span className="isum__row">
      {label !== undefined && <span className="isum__label">{label}</span>}
      <span className="isum__value">{children}</span>
    </span>
  );
}

function SumNone({ children }: { children: ReactNode }) {
  return <span className="isum__none">{children}</span>;
}

/** 45 → "45 s", 120 → "2 min", 150 → "2 min 30 s". */
function fmtSecs(t: T, s: number): string {
  if (s < 60) return t.t("{{n}} s", { n: s });
  const m = Math.floor(s / 60);
  const r = s % 60;
  return r ? t.t("{{m}} min {{s}} s", { m, s: r }) : t.t("{{m}} min", { m });
}

function clip(s: string | undefined, n = 60): string {
  const one = (s ?? "").replace(/\s+/g, " ").trim();
  return one.length > n ? one.slice(0, n - 1) + "…" : one;
}

/** The parts of a node's settings, in the order the inspector shows them. */
function partsOf(node: Node, graph: Graph, onChange: (n: Node) => void, onGraphChange: (g: Graph) => void, t: T, openPart: (id: string) => void): Part[] {
  const parts: Part[] = [];
  switch (node.type) {
    case "start":
      parts.push({ id: "place", title: t.t("Where teams start"), editor: <StartForm node={node} graph={graph} onGraphChange={onGraphChange} /> });
      break;
    case "puzzle":
      if (node.puzzle) parts.push(...puzzleParts(node, node.puzzle, graph, (puzzle) => onChange({ ...node, puzzle }), onGraphChange, t, openPart));
      break;
    case "story": {
      const story = node.story ?? {};
      parts.push({
        id: "content",
        title: t.t("What the team sees"),
        editor: (
          <>
            <BlocksEditor node={node} graph={graph} blocks={story.blocks} textLabel={t.t("Text")} onChange={(blocks) => onChange(withBlocks(node, blocks))} onGraphChange={onGraphChange} />
          </>
        ),
      });
      parts.push({
        id: "rules",
        title: t.t("Rules"),
        summary: (
          <>
            <SumRow label={t.t("Continue")}>{story.zone ? t.t("inside {{zone}}", { zone: zoneName(graph, story.zone) }) : t.t("anywhere")}</SumRow>
            <SumRow label={t.t("Shown")}>{story.once ? t.t("only the first time") : t.t("every time")}</SumRow>
          </>
        ),
        editor: (
          <>
            <ZoneSelect
              graph={graph}
              label={t.t("Only continue inside a zone")}
              value={story.zone}
              onChange={(zone) => onChange({ ...node, story: withOptional({ ...story }, "zone", zone) })}
              here={{
                name: node.title || t.t("Story"),
                set: (zones, slug) => onGraphChange(updateNode({ ...graph, zones }, node.slug, (n) => ({ ...n, story: { ...n.story, zone: slug } }))),
              }}
            />
            <label className="check">
              <input type="checkbox" checked={!!story.once} onChange={(e) => onChange({ ...node, story: withOptional({ ...story }, "once", e.target.checked || undefined) })} />
              <span>
                {t.t("Show only the first time")}
                <span className="field__hint check__hint">{t.t("A team that comes this way again passes straight through — for an arrival or introduction on a branch it returns to.")}</span>
              </span>
            </label>
          </>
        ),
      });
      break;
    }
    case "hub":
      if (node.hub)
        parts.push(
          ...hubParts(node.hub, graph, (hub) => onChange({ ...node, hub }), (home) => {
            // A home screen has no done output; drop a wire from it with it.
            const g = updateNode(graph, node.slug, (n) => ({ ...n, hub: withOptional(withOptional({ ...n.hub! }, "required", home ? undefined : n.hub!.required), "home", home || undefined) }));
            onGraphChange(home ? { ...g, edges: g.edges.filter((e) => !(e.from === node.slug && e.from_port === "done")) } : g);
          }, t),
        );
      break;
    case "gate":
      if (node.gate)
        parts.push({
          id: "condition",
          title: t.t("Pass when"),
          editor: (
            <>
              <p className="field__hint">{t.t("Otherwise the team takes the fail route.")}</p>
              <ConditionEditor graph={graph} value={node.gate.condition} onChange={(condition) => onChange({ ...node, gate: { condition } })} />
            </>
          ),
        });
      break;
    case "switch":
      parts.push(...switchParts(node, graph, onGraphChange, t));
      break;
    case "effect":
      if (node.effect) parts.push({ id: "effect", title: t.t("Does"), editor: <EffectForm cfg={node.effect} graph={graph} onChange={(effect) => onChange({ ...node, effect })} /> });
      break;
    case "end": {
      const end = node.end ?? {};
      parts.push({
        id: "content",
        title: t.t("How it ends"),
        editor: (
          <>
            <Field label={t.t("Outcome")}>
              <select className="field__input" value={end.outcome ?? "complete"} onChange={(e) => onChange({ ...node, end: { ...end, outcome: e.target.value as "complete" | "fail" } })}>
                <option value="complete">{t.t("Success")}</option>
                <option value="fail">{t.t("Failure")}</option>
              </select>
            </Field>
            <BlocksEditor node={node} graph={graph} blocks={end.blocks} textLabel={t.t("Closing text")} onChange={(blocks) => onChange(withBlocks({ ...node, end }, blocks))} onGraphChange={onGraphChange} />
          </>
        ),
      });
      break;
    }
  }
  if (VISIBLE.has(node.type)) {
    const titles = (node.unlocks ?? []).map((s) => (graph.journal ?? []).find((e) => e.slug === s)?.title || s);
    parts.push({
      id: "journal",
      title: t.t("Notes for the journal"),
      count: titles.length || undefined,
      summary: titles.length ? <span className="isum__chips">{titles.map((x) => <span key={x} className="chip concept--journal">{x}</span>)}</span> : <SumNone>{t.t("None")}</SumNone>,
      editor: <UnlocksField graph={graph} value={node.unlocks} onChange={(unlocks) => onChange(withOptional({ ...node }, "unlocks", unlocks.length ? unlocks : undefined))} />,
    });
  }
  return parts;
}

// --- start -----------------------------------------------------------------

/**
 * Where a start is. With one start and no place, teams begin wherever they
 * are; give a start a place, or add a second start, and each team picks
 * one and must stand in its zone to begin.
 */
function StartForm({ node, graph, onGraphChange }: { node: Node; graph: Graph; onGraphChange: (g: Graph) => void }) {
  const t = useT();
  const zones = graph.zones ?? [];
  const slug = node.start?.zone;
  const zone = zones.find((z) => z.slug === slug);
  const starts = graph.nodes.filter((n) => n.type === "start").length;

  const setZone = (g: Graph, z: string | undefined) =>
    onGraphChange(updateNode(g, node.slug, (n) => withOptional({ ...n }, "start", z ? { zone: z } : undefined)));
  const editZone = (next: Zone) => onGraphChange({ ...graph, zones: zones.map((z) => (z.slug === next.slug ? next : z)) });
  const newPlace = () => {
    const used = new Set(zones.map((z) => z.slug));
    let i = 1;
    while (used.has(`start-${i}`)) i++;
    const z: Zone = { slug: `start-${i}`, name: node.title || t.t("Start"), ...nearLastZone(zones), radius_m: 30 };
    setZone({ ...graph, zones: [...zones, z] }, z.slug);
  };

  return (
    <>
      <Field
        label={t.t("Where teams start")}
        hint={
          starts > 1
            ? t.t("This adventure has several starts. Each team picks one, and begins once it stands there.")
            : zone
              ? t.t("Teams begin once they stand here.")
              : t.t("Anywhere: teams begin as soon as the event starts.")
        }
      >
        <select className="field__input" value={slug ?? ""} onChange={(e) => setZone(graph, e.target.value || undefined)}>
          <option value="">{t.t("Anywhere")}</option>
          {zones.map((z) => (
            <option key={z.slug} value={z.slug}>
              {z.name || z.slug}
            </option>
          ))}
        </select>
      </Field>
      {!zone && (
        <button type="button" className="add-btn" onClick={newPlace}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Pick a place on the map")}
        </button>
      )}
      {zone && (
        <>
          <Field label={t.t("Name of the place")} hint={t.t("Teams see this when they choose where to start.")}>
            <input className="field__input" value={zone.name ?? ""} placeholder={zone.slug} onChange={(e) => editZone({ ...zone, name: e.target.value })} />
          </Field>
          <ZoneMap key={zone.slug} zone={zone} onChange={editZone} />
        </>
      )}
    </>
  );
}

// --- puzzle ----------------------------------------------------------------

/** What the answer-kind picker calls each way of answering; "" is typing. */
const ANSWER_KIND_LABEL: Record<AnswerKind | "", string> = {
  "": "Typed in the answer bar",
  scan: "Scanning a QR code only",
  choice: "Options to tap",
  lock: "Combination lock",
  items: "Items from the backpack",
  tiles: "Tile picture",
  search: "Search by place",
  questions: "Quiz questions on the step",
  keys: "Two-key launch: every player turns a key",
};

const KEYBOARD_LABEL: Record<NonNullable<AnswerBar["keyboard"]> | "", string> = {
  "": "Automatic — numbers when every answer is one",
  text: "Letters",
  number: "Numbers",
};

const TRIGGERS: { t: Trigger; label: string }[] = [
  { t: "answer", label: "Correct answer" },
  { t: "attempts", label: "After N wrong guesses" },
  { t: "timeout", label: "Time runs out" },
  { t: "skipped", label: "Team skips" },
  { t: "missing", label: "Not carrying the items" },
];

const MISSING_LABEL: Record<MissingHint | "", string> = {
  "": "That something is missing",
  name: "Which items are missing",
  none: "Nothing",
};

function puzzleParts(node: Node, cfg: PuzzleConfig, graph: Graph, onChange: (c: PuzzleConfig) => void, onGraphChange: (g: Graph) => void, t: T, openPart: (id: string) => void): Part[] {
  const outputs = cfg.outputs ?? [];
  const answer = cfg.answer;
  const setOutput = (i: number, o: Output) => onChange({ ...cfg, outputs: outputs.map((x, j) => (j === i ? o : x)) });

  const addOutput = (trigger: Trigger) => {
    const taken = new Set(outputs.map((o) => o.port));
    let port: string = trigger;
    for (let i = 2; taken.has(port); i++) port = `${trigger}-${i}`;
    const o: Output = { port, label: TRIGGERS.find((x) => x.t === trigger)!.label, trigger };
    if (trigger === "answer") o.matchers = [{ kind: "exact", value: "" }];
    if (trigger === "attempts") o.after_attempts = 3;
    const next = { ...cfg, outputs: [...outputs, o] };
    // A timeout or skip route is meaningless without its threshold; set a
    // sensible one rather than hand the author a validation error.
    if (trigger === "timeout" && !cfg.time_limit_seconds) next.time_limit_seconds = 300;
    if (trigger === "skipped" && !cfg.skip_after_guesses && !cfg.skip_after_seconds) {
      next.skip_after_guesses = 3;
      next.skip_after_seconds = 120;
    }
    onChange(next);
  };

  const parts: Part[] = [
    {
      id: "content",
      title: t.t("What the team sees"),
      editor: (
        <>
          <BlocksEditor node={{ ...node, puzzle: cfg }} graph={graph} blocks={cfg.blocks} textLabel={t.t("Text")} onChange={(blocks) => onChange(withOptional({ ...cfg }, "blocks", blocks.length ? blocks : undefined))} onGraphChange={onGraphChange} />
          <p className="field__hint">{t.t("Shown in this order, above the answer. A team only receives pictures and sounds once it reaches this step.")}</p>
        </>
      ),
    },
  ];

  // How the team answers: the kind, chosen at the top of the part, and
  // that kind's own settings below it.
  const questions = (cfg.blocks ?? []).filter((b) => b.kind === "quiz" || b.kind === "tiebreak");
  // Accepted answers live on each route, so a typed or scanned answer
  // points there rather than letting designers hunt for them here.
  const toRoutes = (
    <Button variant="ghost" size="sm" onClick={() => openPart("routes")}>
      {t.t("Set the accepted answers under Answers and next steps")}
      <ChevronRight size={ICON.sm} />
    </Button>
  );
  let how: Omit<Part, "id" | "editor"> & { form?: ReactNode };
  switch (answer?.kind) {
    case undefined: {
      const bar = cfg.bar ?? {};
      how = {
        title: t.t("Answer bar"),
        hint: t.t("The team types its answer in the bar at the bottom. Which answers count is set under Answers and next steps."),
        summary: (
          <>
            <SumRow label={t.t("Answer bar")}>
              {[bar.placeholder ? `“${bar.placeholder}”` : "", bar.button ? `[${bar.button}]` : ""].filter(Boolean).join(" ") || t.t("Default box and Send button")}
            </SumRow>
            <SumRow label={t.t("Keyboard")}>{t.t(KEYBOARD_LABEL[bar.keyboard ?? ""])}</SumRow>
          </>
        ),
        form: (
          <>
            {toRoutes}
            <BarForm cfg={cfg} onChange={onChange} />
          </>
        ),
      };
      break;
    }
    case "scan":
      how = {
        title: t.t("QR scan"),
        hint: t.t("The answer bar is one scan button. Print a QR code of an accepted answer and hang it at the place. Which codes count is set under Answers and next steps. A team whose camera fails can type the code instead."),
        summary: <SumRow label={t.t("Button")}>{cfg.bar?.button || t.t("Scan the code")}</SumRow>,
        form: (
          <>
            {toRoutes}
            <BarForm cfg={cfg} onChange={onChange} scan />
          </>
        ),
      };
      break;
    case "tiles":
      how = {
        title: t.t("Tile puzzle"),
        hint: t.t("The picture is cut into a grid and shuffled. Teams swap pieces until it is whole."),
        summary: (
          <>
            <SumRow label={t.t("Grid")}>{t.t("{{rows}} × {{cols}}", { rows: answer.rows, cols: answer.cols })}</SumRow>
            <SumRow label={t.t("Picture")}>{answer.image ? t.t("added") : <SumNone>{t.t("none yet")}</SumNone>}</SumRow>
          </>
        ),
        form: <TilesForm tiles={answer} onChange={(tiles) => onChange({ ...cfg, answer: tiles })} />,
      };
      break;
    case "search":
      how = {
        title: t.t("Search by place"),
        hint: t.t("No typing: the team presses Search here, and the route below whose zone it stands in is taken. Anywhere else, the wrong-answer replies say what it found."),
        summary: (
          <>
            <SumRow label={t.t("Names")}>{answer.group ?? t.t("the zones of the routes")}</SumRow>
            <SumRow label={t.t("Again later")}>{cfg.repeatable ? t.t("yes") : t.t("no")}</SumRow>
          </>
        ),
        form: <SearchForm cfg={cfg} search={answer} graph={graph} onChange={onChange} />,
      };
      break;
    case "choice": {
      const graded = isGraded(answer);
      how = {
        title: graded ? t.t("Marked form") : t.t("Multiple choice"),
        hint: graded ? t.t("Every question has a right answer. The team sends all of them in at once.") : t.t("The option tapped is the answer the routes below match."),
        count: answer.fields.length,
        summary: (
          <>
            {answer.fields.map((f, i) => (
              <SumRow key={i} label={f.label || t.t("Question {{n}}", { n: i + 1 })}>
                {f.options.map((o) => (o.value === f.answer ? `✓ ${o.label || o.value}` : o.label || o.value)).join(" · ")}
              </SumRow>
            ))}
          </>
        ),
        form: <ChoicesForm cfg={cfg} choice={answer} onChange={onChange} />,
      };
      break;
    }
    case "lock": {
      const first = outputs.find((o) => o.trigger === "answer");
      const combo = (first?.matchers?.[0]?.value ?? "").split("|").map((v) => answer.symbols.find((x) => x.value === v)?.label || v);
      how = {
        title: t.t("Combination lock"),
        hint: t.t("The team turns each wheel through the same symbols and tries the combination. A wrong one gets the wrong-answer replies."),
        summary: (
          <>
            <SumRow label={t.t("Wheels")}>{answer.wheels}</SumRow>
            <SumRow label={t.t("Opens at")}>{combo.join(" · ")}</SumRow>
          </>
        ),
        form: <LockForm cfg={cfg} lock={answer} onChange={onChange} />,
      };
      break;
    }
    case "items": {
      const itemName = (slug: string) => graph.items?.find((x) => x.slug === slug)?.name || slug;
      how = {
        title: t.t("Item slots"),
        hint: t.t("The team places items from its backpack in the slots. When every slot holds its item, the first route below is taken. A wrong fill gets the wrong-answer replies and costs nothing."),
        count: answer.slots.length,
        summary: (
          <>
            {answer.slots.map((sl, i) => (
              <SumRow key={i} label={sl.label || t.t("Slot {{n}}", { n: i + 1 })}>
                {sl.item ? itemName(sl.item) : <SumNone>{t.t("no item yet")}</SumNone>}
              </SumRow>
            ))}
            <SumRow label={t.t("Items")}>{answer.keep ? t.t("kept") : t.t("used up")}</SumRow>
            <SumRow label={t.t("Without them")}>
              {t.t(MISSING_LABEL[answer.missing ?? ""])}
              {outputs.some((o) => o.trigger === "missing") ? ` · ${t.t("can leave")}` : ""}
            </SumRow>
          </>
        ),
        form: <ItemsForm graph={graph} slots={answer} onChange={(a) => onChange({ ...cfg, answer: a })} />,
      };
      break;
    }
    case "keys":
      how = {
        title: t.t("Two-key launch"),
        hint: t.t("Each player turns their own key on their own phone. When enough keys are turned within the window, the first route below is taken. A team with fewer players needs only as many keys as it has, so one player alone can still finish."),
        summary: (
          <>
            <SumRow label={t.t("Keys")}>{answer.keys ? answer.keys : answer.posts?.length ? t.t("one at each post") : t.t("every player")}</SumRow>
            <SumRow label={t.t("Within")}>{fmtSecs(t, answer.window || 10)}</SumRow>
            {answer.posts?.length ? (
              <SumRow label={t.t("Posts")}>
                {answer.posts
                  .map((z, i) => {
                    const role = answer.post_roles?.[i];
                    const name = role ? (graph.roles ?? []).find((r) => r.slug === role)?.name || role : "";
                    return name ? t.t("{{role}} at {{place}}", { role: name, place: zoneName(graph, z) }) : zoneName(graph, z);
                  })
                  .join(", ")}
              </SumRow>
            ) : null}
          </>
        ),
        form: <KeysForm graph={graph} keys={answer} onChange={(a) => onChange({ ...cfg, answer: a })} />,
      };
      break;
    case "questions":
      how = {
        title: t.t("Quiz questions"),
        hint: t.t("The team answers the quiz questions in What the team sees, then continues by the first route below. The questions score when the team finishes, as on a quiz walk."),
        count: questions.length,
        summary: questions.length ? (
          <>
            {questions.map((b) => (
              <SumRow key={b.id} label={b.kind === "tiebreak" ? t.t("Tie-breaker") : t.t("Question")}>
                {clip(b.text) || <SumNone>{t.t("empty")}</SumNone>}
              </SumRow>
            ))}
          </>
        ) : (
          <SumNone>{t.t("None yet — add a quiz question under What the team sees")}</SumNone>
        ),
      };
      break;
  }
  parts.push({
    id: "answer",
    ...how,
    editor: (
      <>
        <Field label={t.t("How the team answers")}>
          <select
            className="field__input"
            value={answer?.kind ?? ""}
            onChange={(e) => onChange(withAnswerKind(cfg, (e.target.value || undefined) as AnswerKind | undefined))}
          >
            {ANSWER_KINDS.map((k) => (
              <option key={k ?? ""} value={k ?? ""}>
                {t.t(ANSWER_KIND_LABEL[k ?? ""])}
              </option>
            ))}
          </select>
        </Field>
        {how.form}
      </>
    ),
  });

  const triggerLabel = (o: Output) => t.t(TRIGGERS.find((x) => x.t === o.trigger)?.label ?? o.trigger);
  parts.push({
    id: "routes",
    title: t.t("Answers and next steps"),
    hint: t.t("Where the team goes next. Each route gets its own dot on the card, to connect to a different step."),
    count: outputs.length,
    ports: true,
    summary: (
      <>
        {outputs.map((o, i) => {
          const accepts =
            o.trigger !== "answer"
              ? o.trigger === "attempts"
                ? t.t("after {{n}} wrong guesses", { n: o.after_attempts ?? 0 })
                : triggerLabel(o)
              : answer?.kind === "tiles" || answer?.kind === "lock" || isGraded(answer) || answer?.kind === "search" || answer?.kind === "questions" || answer?.kind === "items" || answer?.kind === "keys"
                ? triggerLabel(o)
                : (o.matchers ?? []).map(matcherLabel).join(" / ") || t.t("no answer yet");
          const extra = [o.points ? t.t("{{n}} p", { n: o.points }) : "", o.zone ? (o.inverted ? t.t("outside {{zone}}", { zone: zoneName(graph, o.zone) }) : t.t("in {{zone}}", { zone: zoneName(graph, o.zone) })) : ""]
            .filter(Boolean)
            .join(" · ");
          return (
            <SumRow key={i} label={o.label || o.port}>
              {accepts}
              {extra && <span className="isum__extra"> · {extra}</span>}
            </SumRow>
          );
        })}
      </>
    ),
    editor: (
      <>
        {outputs.map((o, i) => (
          <div key={i} className="box">
            <div className="box__row box__row--wrap box__row--route">
              <input
                className="field__input"
                value={o.label ?? ""}
                placeholder={o.port}
                onChange={(e) => setOutput(i, { ...o, label: e.target.value })}
                aria-label={t.t("Route label")}
              />
              <select className="field__input" value={o.trigger} onChange={(e) => setOutput(i, { ...o, trigger: e.target.value as Trigger })} aria-label={t.t("When")}>
                {TRIGGERS.filter((x) => x.t !== "missing" || answer?.kind === "items" || o.trigger === "missing").map((x) => (
                  <option key={x.t} value={x.t}>
                    {t.t(x.label)}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="icon-remove"
                disabled={outputs.length <= 1}
                title={outputs.length <= 1 ? t.t("A puzzle needs at least one way out.") : undefined}
                onClick={() => onChange({ ...cfg, outputs: outputs.filter((_, j) => j !== i) })}
                aria-label={t.t("Remove")}
              >
                <Trash2 size={ICON.sm} />
              </button>
            </div>

            {o.trigger === "missing" && (
              <p className="field__hint">
                {t.t("While the team lacks an item the slots need, the phone offers this route as a button with its label. The puzzle stays unanswered, so it is shown again when the team comes back.")}
              </p>
            )}

            {o.trigger === "answer" && (
              <>
                {answer?.kind === "tiles" ? (
                  <p className="field__hint">{t.t("Taken when the picture is whole.")}</p>
                ) : answer?.kind === "lock" ? (
                  <p className="field__hint">{t.t("Taken for the combination set above: {{combo}}", { combo: (o.matchers?.[0]?.value ?? "").split("|").join(" · ") })}</p>
                ) : isGraded(answer) ? (
                  <p className="field__hint">{t.t("Always taken when the form is sent in; its points are shared between the questions answered right.")}</p>
                ) : answer?.kind === "search" ? (
                  <p className="field__hint">{t.t("Taken when the team searches inside this route's zone. Without a zone, it is taken anywhere else.")}</p>
                ) : answer?.kind === "questions" ? (
                  <p className="field__hint">{t.t("Taken when every question is answered and the team continues. Only the first such route is ever taken.")}</p>
                ) : answer?.kind === "items" ? (
                  <p className="field__hint">{t.t("Taken when every slot holds its item. Only the first such route is ever taken.")}</p>
                ) : (
                  <MatcherList matchers={o.matchers ?? []} onChange={(matchers) => setOutput(i, { ...o, matchers })} />
                )}
                <div className="box__row">
                  <NumberField label={t.t("Points")} value={o.points} onChange={(points) => setOutput(i, withOptional({ ...o }, "points", points))} />
                </div>
                {/* A search's routes are chosen by where the team stands, so
                    their zone is the point; elsewhere it is rarely needed. */}
                <details className="box__more" open={!!o.zone || answer?.kind === "search"}>
                  <summary>{t.t("Only counts in a zone")}</summary>
                  <ZoneSelect
                    graph={graph}
                    label={t.t("Only counts…")}
                    value={o.zone}
                    onChange={(zone) => setOutput(i, withOptional({ ...o }, "zone", zone))}
                  />
                  {o.zone && (
                    <label className="check">
                      <input type="checkbox" checked={!!o.inverted} onChange={(e) => setOutput(i, { ...o, inverted: e.target.checked })} />
                      {t.t("…when the team is NOT inside it")}
                    </label>
                  )}
                </details>
              </>
            )}
            {o.trigger === "attempts" && (
              <NumberField label={t.t("After this many wrong guesses")} value={o.after_attempts} onChange={(n) => setOutput(i, withOptional({ ...o }, "after_attempts", n))} />
            )}
          </div>
        ))}
        <div className="add-row">
          {answer?.kind !== "tiles" && answer?.kind !== "questions" && (
            <button type="button" className={`add-btn concept--${TRIGGER.answer}`} onClick={() => addOutput("answer")}>
              <Plus size={ICON.sm} aria-hidden="true" />
              {answer?.kind === "search" ? t.t("Another place") : t.t("Another answer")}
            </button>
          )}
          <button type="button" className={`add-btn concept--${TRIGGER.attempts}`} onClick={() => addOutput("attempts")}>
            <Plus size={ICON.sm} aria-hidden="true" />
            {t.t("After wrong guesses")}
          </button>
          <button type="button" className={`add-btn concept--${TRIGGER.timeout}`} onClick={() => addOutput("timeout")}>
            <Plus size={ICON.sm} aria-hidden="true" />
            {t.t("When time runs out")}
          </button>
          <button type="button" className={`add-btn concept--${TRIGGER.skipped}`} onClick={() => addOutput("skipped")}>
            <Plus size={ICON.sm} aria-hidden="true" />
            {t.t("When the team skips")}
          </button>
        </div>
      </>
    ),
  });

  const responses = cfg.responses ?? [];
  parts.push({
    id: "replies",
    title: t.t("Wrong-answer replies"),
    hint: t.t("Shown after a wrong guess. Each applies from its number of wrong guesses on; the last repeats."),
    count: responses.length || undefined,
    summary: responses.length ? (
      <>
        {responses.map((r, i) => (
          <SumRow
            key={i}
            label={[t.t("From guess {{n}}", { n: r.after_attempts ?? 1 }), r.lockout_seconds ? t.t("locks {{time}}", { time: fmtSecs(t, r.lockout_seconds) }) : ""]
              .filter(Boolean)
              .join(" · ")}
          >
            {clip(r.text) || <SumNone>{t.t("empty")}</SumNone>}
          </SumRow>
        ))}
      </>
    ) : (
      <SumNone>{t.t("None — the team sees the standard reply")}</SumNone>
    ),
    editor: (
      <ListEditor<Response>
        items={responses}
        make={() => ({ after_attempts: responses.length + 1, text: "" })}
        onChange={(next) => onChange(withOptional({ ...cfg }, "responses", next.length ? next : undefined))}
        render={(r, set) => (
          <div className="box__row">
            <NumberField label={t.t("From guess")} value={r.after_attempts} onChange={(n) => set(withOptional({ ...r }, "after_attempts", n))} />
            <input className="field__input" value={r.text} placeholder={t.t("Not quite…")} onChange={(e) => set({ ...r, text: e.target.value })} aria-label={t.t("Reply")} />
            <NumberField label={t.t("Lockout (s)")} value={r.lockout_seconds} onChange={(n) => set(withOptional({ ...r }, "lockout_seconds", n))} />
          </div>
        )}
        addLabel={t.t("Add reply")}
      />
    ),
  });

  const hints = cfg.hints ?? [];
  parts.push({
    id: "hints",
    title: t.t("Hints"),
    count: hints.length || undefined,
    summary: hints.length ? (
      <>
        {hints.map((h, i) => (
          <SumRow key={i} label={[t.t("after {{time}}", { time: fmtSecs(t, h.delay_seconds ?? 0) }), h.cost ? t.t("−{{n}} p", { n: h.cost }) : ""].filter(Boolean).join(" · ")}>
            {clip(h.text) || <SumNone>{t.t("empty")}</SumNone>}
          </SumRow>
        ))}
      </>
    ) : (
      <SumNone>{t.t("None")}</SumNone>
    ),
    editor: (
      <ListEditor<Hint>
        items={hints}
        make={() => ({ text: "", delay_seconds: 120 })}
        onChange={(next) => onChange(withOptional({ ...cfg }, "hints", next.length ? next : undefined))}
        render={(h, set) => (
          <>
            <MarkdownArea className="field__input" rows={2} value={h.text} onChange={(e) => set({ ...h, text: e.target.value })} aria-label={t.t("Hint")} />
            <div className="box__row">
              <NumberField label={t.t("Available after (s)")} value={h.delay_seconds} onChange={(n) => set(withOptional({ ...h }, "delay_seconds", n))} />
              <NumberField label={t.t("Costs points")} value={h.cost} onChange={(n) => set(withOptional({ ...h }, "cost", n))} />
            </div>
            <MediaEditor media={h.media} onChange={(media) => set(withOptional({ ...h }, "media", media))} />
          </>
        )}
        addLabel={t.t("Add hint")}
      />
    ),
  });

  // The settings most puzzles never need: the digest names only what is set.
  const rules = [
    cfg.zone && [t.t("Answerable"), t.t("inside {{zone}}", { zone: zoneName(graph, cfg.zone) })],
    cfg.time_limit_seconds && [t.t("Time limit"), fmtSecs(t, cfg.time_limit_seconds)],
    cfg.lockout_seconds && [t.t("Lockout after wrong"), fmtSecs(t, cfg.lockout_seconds)],
    (cfg.skip_after_guesses || cfg.skip_after_seconds) && [
      t.t("Skip"),
      [cfg.skip_after_guesses ? t.t("after {{n}} guesses", { n: cfg.skip_after_guesses }) : "", cfg.skip_after_seconds ? t.t("after {{time}}", { time: fmtSecs(t, cfg.skip_after_seconds) }) : ""].filter(Boolean).join(t.t(" and ")),
    ],
    cfg.repeatable && answer?.kind !== "search" && [t.t("Coming back"), t.t("asked again")],
    cfg.unanimous && [t.t("Unanimous"), t.t("every player gives the answer")],
    cfg.by && [t.t("Answered by"), graph.roles?.find((r) => r.slug === cfg.by)?.name || cfg.by],
  ].filter((x): x is string[] => !!x);
  parts.push({
    id: "rules",
    title: t.t("Place, time limits, skipping"),
    summary: rules.length ? (
      <>
        {rules.map(([label, value]) => (
          <SumRow key={label} label={label}>
            {value}
          </SumRow>
        ))}
      </>
    ) : (
      <SumNone>{t.t("Anywhere, no time limit, no skipping")}</SumNone>
    ),
    editor: (
      <>
        <ZoneSelect
          graph={graph}
          label={t.t("Only answerable inside a zone")}
          value={cfg.zone}
          onChange={(zone) => onChange(withOptional({ ...cfg }, "zone", zone))}
          here={puzzleHere(node, graph, onGraphChange, t)}
        />
        <div className="box__row box__row--wrap">
          <NumberField label={t.t("Time limit (s)")} value={cfg.time_limit_seconds} onChange={(n) => onChange(withOptional({ ...cfg }, "time_limit_seconds", n))} />
          <NumberField label={t.t("Lockout after wrong (s)")} value={cfg.lockout_seconds} onChange={(n) => onChange(withOptional({ ...cfg }, "lockout_seconds", n))} />
        </div>
        <div className="box__row box__row--wrap">
          <NumberField label={t.t("Skip after guesses")} value={cfg.skip_after_guesses} onChange={(n) => onChange(withOptional({ ...cfg }, "skip_after_guesses", n))} />
          <NumberField label={t.t("…and after (s)")} value={cfg.skip_after_seconds} onChange={(n) => onChange(withOptional({ ...cfg }, "skip_after_seconds", n))} />
        </div>
        <label className="check">
          <input type="checkbox" checked={!!cfg.repeatable} onChange={(e) => onChange(withOptional({ ...cfg }, "repeatable", e.target.checked || undefined))} />
          {t.t("Ask again if the team comes back")}
        </label>
        {(graph.roles ?? []).length > 0 && !cfg.unanimous && answer?.kind !== "keys" && (
          <Field label={t.t("Only this role can answer")}>
            <select className="field__input" value={cfg.by ?? ""} onChange={(e) => onChange(withOptional({ ...cfg }, "by", e.target.value || undefined))}>
              <option value="">{t.t("— anyone on the team —")}</option>
              {(graph.roles ?? []).map((r) => (
                <option key={r.slug} value={r.slug}>
                  {r.name || r.slug}
                </option>
              ))}
            </select>
          </Field>
        )}
        {usesMatchers(cfg) && !cfg.by && (
          <>
            <label className="check">
              <input type="checkbox" checked={!!cfg.unanimous} onChange={(e) => onChange(withOptional({ ...cfg }, "unanimous", e.target.checked || undefined))} />
              {t.t("Every player must give the answer")}
            </label>
            <p className="field__hint">
              {t.t("Each player answers on their own phone, and the answer counts once all of them have given one that leads the same way. Until then the phones say how many agree. A player alone answers for the team.")}
            </p>
          </>
        )}
      </>
    ),
  });
  return parts;
}

/** A two-key launch: how many keys, how close together, and where. */
function KeysForm({ graph, keys, onChange }: { graph: Graph; keys: AnswerOf<"keys">; onChange: (a: AnswerOf<"keys">) => void }) {
  const t = useT();
  const posts = keys.posts ?? [];
  const postRoles = keys.post_roles ?? [];
  const roles = (graph.roles ?? []).filter((r) => !r.secret);
  // Posts and their roles go together: a role stays with its post as
  // others come and go, and no trailing empty roles are kept.
  const setPosts = (nextPosts: string[], nextRoles: string[]) => {
    const trimmed = nextRoles.slice(0, nextPosts.length);
    while (trimmed.length && !trimmed[trimmed.length - 1]) trimmed.pop();
    onChange(withOptional(withOptional({ ...keys }, "posts", nextPosts.length ? nextPosts : undefined), "post_roles", trimmed.length ? trimmed : undefined));
  };
  return (
    <>
      <div className="box__row box__row--wrap">
        <NumberField label={t.t("Keys (empty: every player)")} value={keys.keys} onChange={(n) => onChange(withOptional({ ...keys }, "keys", n && n > 0 ? n : undefined))} />
        <NumberField label={t.t("Within (s)")} value={keys.window} onChange={(n) => onChange(withOptional({ ...keys }, "window", n && n > 0 ? n : undefined))} />
      </div>
      <Field label={t.t("Posts: turn each key at its own place (optional)")}>
        <div className="cond__days">
          {(graph.zones ?? []).length === 0 && <span className="field__hint">{t.t("— no zones yet —")}</span>}
          {(graph.zones ?? []).filter((z) => !z.group).map((z) => {
            const on = posts.includes(z.slug);
            return (
              <label key={z.slug} className="chip">
                <input
                  type="checkbox"
                  checked={on}
                  onChange={() => {
                    if (on) {
                      const i = posts.indexOf(z.slug);
                      setPosts(posts.filter((x) => x !== z.slug), postRoles.filter((_, j) => j !== i));
                    } else {
                      setPosts([...posts, z.slug], postRoles);
                    }
                  }}
                />
                {z.name || z.slug}
              </label>
            );
          })}
        </div>
      </Field>
      {posts.length > 0 && roles.length > 0 && (
        <Field label={t.t("Who turns the key at each post (optional)")}>
          {posts.map((slug, i) => (
            <label key={slug} className="box__row">
              <span className="field__hint">{zoneName(graph, slug)}</span>
              <select
                className="field__input"
                value={postRoles[i] ?? ""}
                onChange={(e) => {
                  const next = posts.map((_, j) => (j === i ? e.target.value : (postRoles[j] ?? "")));
                  setPosts(posts, next);
                }}
              >
                <option value="">{t.t("Anyone")}</option>
                {roles.map((r) => (
                  <option key={r.slug} value={r.slug}>
                    {r.name || r.slug}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </Field>
      )}
    </>
  );
}

const MATCHERS: { k: MatcherKind; label: string }[] = [
  { k: "exact", label: "Exactly" },
  { k: "contains", label: "Contains" },
  { k: "regex", label: "Pattern (regex)" },
  { k: "interval", label: "Number between" },
  { k: "near", label: "Number near (partial credit)" },
  { k: "set", label: "Several of a list" },
  { k: "anything", label: "Anything" },
];

function MatcherList({ matchers, onChange }: { matchers: Matcher[]; onChange: (m: Matcher[]) => void }) {
  const t = useT();
  return (
    <ListEditor<Matcher>
      items={matchers}
      make={() => ({ kind: "exact", value: "" })}
      onChange={onChange}
      render={(m, set) => (
        <div className="box__row box__row--wrap">
          <select className="field__input" value={m.kind} onChange={(e) => set({ kind: e.target.value as MatcherKind })} aria-label={t.t("Accept")}>
            {MATCHERS.map((x) => (
              <option key={x.k} value={x.k}>
                {t.t(x.label)}
              </option>
            ))}
          </select>
          {(m.kind === "exact" || m.kind === "contains" || m.kind === "regex") && (
            <input className="field__input" value={m.value ?? ""} placeholder={t.t("answer")} onChange={(e) => set({ ...m, value: e.target.value })} aria-label={t.t("Answer")} />
          )}
          {m.kind === "interval" && (
            <>
              <NumberField label={t.t("From")} value={m.low} onChange={(n) => set(withOptional({ ...m }, "low", n))} />
              <NumberField label={t.t("To")} value={m.high} onChange={(n) => set(withOptional({ ...m }, "high", n))} />
            </>
          )}
          {m.kind === "near" && (
            <>
              <NumberField label={t.t("Target")} value={m.target} onChange={(n) => set(withOptional({ ...m }, "target", n))} />
              <NumberField label={t.t("± for no credit")} value={m.tolerance} onChange={(n) => set(withOptional({ ...m }, "tolerance", n))} />
            </>
          )}
          {m.kind === "set" && (
            <>
              <input
                className="field__input"
                value={(m.members ?? []).join(", ")}
                placeholder={t.t("red, green, blue")}
                onChange={(e) => set({ ...m, members: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })}
                aria-label={t.t("Members")}
              />
              <NumberField label={t.t("How many needed")} value={m.required} onChange={(n) => set(withOptional({ ...m }, "required", n))} />
              <label className="check">
                <input type="checkbox" checked={!!m.ordered} onChange={(e) => set(withOptional({ ...m }, "ordered", e.target.checked || undefined))} />
                {t.t("In order")}
              </label>
            </>
          )}
        </div>
      )}
      addLabel={t.t("Also accept")}
    />
  );
}

// --- hub -------------------------------------------------------------------

const POLICIES: { p: Policy; label: string; hint: string }[] = [
  { p: "choice", label: "Team chooses", hint: "The team picks which branch to do next." },
  { p: "sequence", label: "In order", hint: "Branches in the order listed." },
  { p: "shuffle", label: "Shuffled per team", hint: "Each team gets its own order, so they don't all go to the same place first." },
  { p: "spread", label: "Least busy", hint: "Send each team to the branch with the fewest teams in it." },
  { p: "capacity", label: "Capacity limits", hint: "Respect a maximum number of teams per branch." },
  { p: "random", label: "Random", hint: "Random, but reproducible for each team." },
];

function hubParts(cfg: HubConfig, graph: Graph, onChange: (c: HubConfig) => void, onHome: (home: boolean) => void, t: T): Part[] {
  const branches = cfg.branches ?? [];
  const policy = POLICIES.find((p) => p.p === cfg.policy);
  const setBranch = (i: number, b: Branch) => onChange({ ...cfg, branches: branches.map((x, j) => (j === i ? b : x)) });
  const parts: Part[] = [
    {
      id: "setup",
      title: t.t("How branches are picked"),
      summary: (
        <>
          <SumRow label={t.t("Picked")}>{policy ? t.t(policy.label) : cfg.policy}</SumRow>
          {cfg.home ? (
            <SumRow label={t.t("Home screen")}>{t.t("yes — teams keep coming back")}</SumRow>
          ) : (
            <SumRow label={t.t("Moves on after")}>{cfg.required ? t.t("{{n}} of {{total}} branches", { n: cfg.required, total: branches.length }) : t.t("all branches")}</SumRow>
          )}
          {cfg.policy === "choice" && cfg.hide_locked && <SumRow label={t.t("Closed branches")}>{t.t("hidden")}</SumRow>}
        </>
      ),
      editor: (
        <>
          <Field label={t.t("How branches are picked")} hint={policy ? t.t(policy.hint) : undefined}>
            <select className="field__input" value={cfg.policy} onChange={(e) => onChange({ ...cfg, policy: e.target.value as Policy })}>
              {POLICIES.map((p) => (
                <option key={p.p} value={p.p}>
                  {t.t(p.label)}
                </option>
              ))}
            </select>
          </Field>
          {cfg.policy === "choice" && (
            <label className="check">
              <input type="checkbox" checked={!!cfg.home} onChange={(e) => onHome(e.target.checked)} />
              <span>
                {t.t("Home screen")}
                <span className="field__hint check__hint">
                  {t.t("Teams keep coming back here. Branches stay open after a visit, teams can go back here from anywhere inside them, and it never finishes — end the adventure from one of its branches.")}
                </span>
              </span>
            </label>
          )}
          {!cfg.home && (
            <Field label={t.t("Branches required before moving on")} hint={t.t("Leave empty for all of them.")}>
              <input
                className="field__input"
                type="number"
                min={0}
                max={branches.length}
                value={cfg.required ?? ""}
                onChange={(e) => onChange(withOptional({ ...cfg }, "required", e.target.value === "" ? undefined : Number(e.target.value)))}
              />
            </Field>
          )}
          {cfg.policy === "choice" && (
            <label className="check">
              <input type="checkbox" checked={!!cfg.hide_locked} onChange={(e) => onChange(withOptional({ ...cfg }, "hide_locked", e.target.checked || undefined))} />
              {t.t("Hide branches that are not open yet")}
            </label>
          )}
        </>
      ),
    },
  ];
  if (cfg.policy === "choice")
    parts.push({
      id: "text",
      title: t.t("What the team sees"),
      editor: (
        <Field label={t.t("Text")} hint={t.t("Shown above the branches. Markdown. Empty says “Choose where to go next.”")}>
          <MarkdownArea className="field__input" rows={3} value={cfg.text ?? ""} onChange={(e) => onChange(withOptional({ ...cfg }, "text", e.target.value || undefined))} />
        </Field>
      ),
    });
  parts.push({
    id: "branches",
    title: t.t("Branches"),
    count: branches.length,
    ports: true,
    summary: (
      <>
        {branches.map((b) => (
          <SumRow key={b.port} label={b.label || b.port}>
            {[
              b.note ? clip(b.note, 40) : "",
              cfg.policy === "capacity" && b.capacity ? t.t("max {{n}} teams", { n: b.capacity }) : "",
              b.condition?.op ? t.t("only when…") : "",
            ]
              .filter(Boolean)
              .join(" · ") || <SumNone>{t.t("always offered")}</SumNone>}
          </SumRow>
        ))}
      </>
    ),
    editor: (
      <>
      {branches.map((b, i) => (
        <div key={b.port} className="box">
          <div className="box__row">
            <input
              className="field__input"
              value={b.label ?? ""}
              placeholder={b.port}
              onChange={(e) => setBranch(i, { ...b, label: e.target.value })}
              aria-label={t.t("Branch label")}
            />
            {cfg.policy === "capacity" && (
              <NumberField
                label={t.t("Max teams")}
                value={b.capacity}
                onChange={(n) => setBranch(i, withOptional({ ...b }, "capacity", n))}
              />
            )}
            <button
              type="button"
              className="icon-remove"
              disabled={branches.length <= 1}
              onClick={() => onChange({ ...cfg, branches: branches.filter((_, j) => j !== i) })}
              aria-label={t.t("Remove")}
              title={t.t("Remove")}
            >
              <Trash2 size={ICON.sm} />
            </button>
          </div>
          {cfg.policy === "choice" && (
            <details className="box__more" open={!!(b.note || b.image)}>
              <summary>{t.t("Tile: subtitle and picture")}</summary>
              <input
                className="field__input"
                value={b.note ?? ""}
                placeholder={t.t("A line under the label")}
                aria-label={t.t("Subtitle")}
                onChange={(e) => setBranch(i, withOptional({ ...b }, "note", e.target.value || undefined))}
              />
              <MediaEditor
                single
                media={b.image ? [b.image] : undefined}
                onChange={(m) => setBranch(i, withOptional({ ...b }, "image", m?.[0]))}
              />
            </details>
          )}
          <details className="box__more" open={!!b.condition?.op}>
            <summary>{t.t("Only offer when…")}</summary>
            <ConditionEditor
              graph={graph}
              value={b.condition ?? { op: "" }}
              onChange={(c) => setBranch(i, withOptional({ ...b }, "condition", c.op ? c : undefined))}
            />
          </details>
        </div>
      ))}
      <button
        type="button"
        className="add-btn"
        onClick={() => {
          const port = nextBranchPort(branches.map((b) => b.port));
          onChange({ ...cfg, branches: [...branches, { port, label: port.toUpperCase() }] });
        }}
      >
        <Plus size={ICON.sm} aria-hidden="true" />
        {t.t("Add branch")}
      </button>
    
      </>
    ),
  });
  return parts;
}

/**
 * A crossroads (a switch node): which answer it reads, and the paths it
 * can take. A path picks answers from the puzzle's own accepted answers,
 * by reference — so correcting a spelling in the puzzle keeps the path
 * working. Typed answers remain, folded away, for open questions whose
 * puzzle accepts anything. "Any other answer" is always there.
 */
function switchParts(node: Node, graph: Graph, onGraphChange: (g: Graph) => void, t: T): Part[] {
  const cfg: SwitchConfig = node.switch ?? {};
  const cases = cfg.cases ?? [];
  const puzzles = graph.nodes.filter((n) => n.type === "puzzle");
  // Answers come from the chosen puzzle, or from the puzzles wired
  // straight in when the crossroads reads "the answer just given".
  const sources = cfg.from ? [cfg.from] : puzzlesInto(graph, node.slug);
  const options = sources.flatMap((s) => answerOptions(graph, s));
  const titleOf = (slug: string) => nodeBySlug(graph, slug)?.title || slug;

  const setCfg = (g: Graph, next: SwitchConfig) => onGraphChange(updateNode(g, node.slug, (n) => ({ ...n, switch: next })));
  const setCase = (g: Graph, i: number, c: Case) => setCfg(g, { ...cfg, cases: cases.map((x, j) => (j === i ? c : x)) });

  const parts: Part[] = [
    {
      id: "source",
      title: t.t("Which answer?"),
      editor: (
        <>
          <select className="field__input" value={cfg.from ?? ""} onChange={(e) => setCfg(graph, withOptional({ ...cfg }, "from", e.target.value || undefined))} aria-label={t.t("Which answer?")}>
            <option value="">{t.t("The answer just given")}</option>
            {puzzles.map((p) => (
              <option key={p.slug} value={p.slug}>
                {t.t("The answer to “{{title}}”", { title: p.title || p.slug })}
              </option>
            ))}
          </select>
          {sources.length === 0 && (
            <p className="field__hint">{t.t("Connect a puzzle to this crossroads, or pick one above, to choose from its answers.")}</p>
          )}
        </>
      ),
    },
    {
      id: "paths",
      title: t.t("Paths"),
      hint: t.t("Checked from the top; the first that fits the answer is taken. Anything else takes “Any other answer”."),
      count: cases.length,
      ports: true,
      summary: (
        <>
          {cases.map((c) => (
            <SumRow key={c.port} label={c.label || c.port}>
              {[...(c.answers ?? []).map((ref) => resolveAnswer(graph, ref)?.label ?? t.t("(answer removed)")), ...(c.matchers ?? []).map(matcherLabel)].join(" / ") || (
                <SumNone>{t.t("no answers yet")}</SumNone>
              )}
            </SumRow>
          ))}
          <SumRow label={t.t("Any other answer")}>
            <SumNone>{t.t("everything else")}</SumNone>
          </SumRow>
        </>
      ),
      editor: (
        <>
      {cases.map((c, i) => {
        const chosen = c.answers ?? [];
        const free = options.filter((o) => !o.ref || !chosen.includes(o.ref));
        return (
          <div key={c.port} className="box">
            <div className="box__row">
              <input
                className="field__input"
                value={c.label ?? ""}
                placeholder={c.port}
                onChange={(e) => setCase(graph, i, { ...c, label: e.target.value })}
                aria-label={t.t("Path name")}
              />
              <button type="button" className="icon-remove" onClick={() => setCfg(graph, { ...cfg, cases: cases.filter((_, j) => j !== i) })} aria-label={t.t("Remove")} title={t.t("Remove")}>
                <Trash2 size={ICON.sm} />
              </button>
            </div>

            <div className="answer-chips">
              {chosen.map((ref) => {
                const a = resolveAnswer(graph, ref);
                return (
                  <span key={ref} className={"answer-chip" + (a ? "" : " is-gone")}>
                    {a ? a.label : t.t("(answer removed)")}
                    {a && sources.length > 1 && <small> · {titleOf(a.node)}</small>}
                    <button
                      type="button"
                      aria-label={t.t("Remove")}
                      onClick={() => setCase(graph, i, withOptional({ ...c }, "answers", chosen.filter((r) => r !== ref).length ? chosen.filter((r) => r !== ref) : undefined))}
                    >
                      ×
                    </button>
                  </span>
                );
              })}
              {free.length > 0 && (
                <select
                  className="field__input answer-add"
                  value=""
                  aria-label={t.t("Add an answer")}
                  onChange={(e) => {
                    const opt = free[Number(e.target.value)];
                    if (!opt) return;
                    const { graph: g, ref } = referAnswer(graph, opt);
                    setCase(g, i, { ...c, answers: [...chosen, ref] });
                  }}
                >
                  <option value="">{t.t("+ Add an answer…")}</option>
                  {free.map((o, k) => (
                    <option key={k} value={k}>
                      {sources.length > 1 ? `${titleOf(o.node)}: ${o.label}` : o.label}
                    </option>
                  ))}
                </select>
              )}
            </div>

            <details className="box__more" open={(c.matchers?.length ?? 0) > 0 || options.length === 0}>
              <summary>{t.t("Or type answers")}</summary>
              <p className="field__hint">{t.t("For open questions, where the puzzle accepts any answer. Typed answers do not follow spelling fixes in the puzzle.")}</p>
              <MatcherList
                matchers={c.matchers ?? []}
                onChange={(matchers) => setCase(graph, i, withOptional({ ...c }, "matchers", matchers.length ? matchers : undefined))}
              />
            </details>
          </div>
        );
      })}
      <button
        type="button"
        className="add-btn"
        onClick={() => {
          const port = nextBranchPort(cases.map((c) => c.port));
          setCfg(graph, { ...cfg, cases: [...cases, { port, label: port.toUpperCase() }] });
        }}
      >
        <Plus size={ICON.sm} aria-hidden="true" />
        {t.t("Add path")}
      </button>
    
        </>
      ),
    },
  ];
  return parts;
}

/** a, b, … z, then branch-27, branch-28 — never a port already in use. */
function nextBranchPort(taken: string[]): string {
  const used = new Set(taken);
  for (let c = 0; c < 26; c++) {
    const p = String.fromCharCode(97 + c);
    if (!used.has(p)) return p;
  }
  for (let i = 27; ; i++) if (!used.has(`branch-${i}`)) return `branch-${i}`;
}

// --- effect ----------------------------------------------------------------

function EffectForm({ cfg, graph, onChange }: { cfg: EffectConfig; graph: Graph; onChange: (c: EffectConfig) => void }) {
  const t = useT();
  const openThemes = useContext(OpenThemes);
  const saved = graph.themes ?? [];
  return (
    <>
      <Field label={t.t("Does")}>
        <select className="field__input" value={cfg.kind} onChange={(e) => onChange({ kind: e.target.value as EffectConfig["kind"] })}>
          <option value="score">{t.t("Award points")}</option>
          <option value="grant_item">{t.t("Give an item")}</option>
          <option value="counter">{t.t("Change a shared counter")}</option>
          <option value="journal">{t.t("Unlock a journal entry")}</option>
          <option value="email">{t.t("Send an email")}</option>
          <option value="webhook">{t.t("Call a webhook")}</option>
          <option value="theme">{t.t("Change the theme")}</option>
        </select>
      </Field>
      {cfg.kind === "score" && <NumberField label={t.t("Points (negative to deduct)")} value={cfg.points} onChange={(n) => onChange(withOptional({ ...cfg }, "points", n))} />}
      {cfg.kind === "grant_item" && (
        <div className="box__row">
          <ResourceSelect label={t.t("Item")} value={cfg.item} options={graph.items ?? []} onChange={(item) => onChange({ ...cfg, item })} />
          <NumberField label={t.t("How many")} value={cfg.qty} onChange={(n) => onChange(withOptional({ ...cfg }, "qty", n))} />
        </div>
      )}
      {cfg.kind === "counter" && (
        <div className="box__row box__row--wrap">
          <ResourceSelect label={t.t("Counter")} value={cfg.counter} options={graph.counters ?? []} onChange={(counter) => onChange({ ...cfg, counter })} />
          <select className="field__input" value={cfg.op ?? "add"} onChange={(e) => onChange({ ...cfg, op: e.target.value as "add" | "set" })} aria-label={t.t("Operation")}>
            <option value="add">{t.t("add")}</option>
            <option value="set">{t.t("set to")}</option>
          </select>
          <NumberField label={t.t("Value")} value={cfg.value} onChange={(n) => onChange(withOptional({ ...cfg }, "value", n))} />
        </div>
      )}
      {cfg.kind === "journal" && (
        <ResourceSelect label={t.t("Entry")} value={cfg.journal_entry} options={(graph.journal ?? []).map((j) => ({ slug: j.slug, name: j.title ?? j.slug }))} onChange={(journal_entry) => onChange({ ...cfg, journal_entry })} />
      )}
      {cfg.kind === "email" && (
        <>
          <Field label={t.t("Subject")}>
            <input className="field__input" value={cfg.subject ?? ""} onChange={(e) => onChange({ ...cfg, subject: e.target.value })} />
          </Field>
          <Field label={t.t("Message")} hint={t.t("Sent to the team's contact email after its move; teams without one get nothing. {{team}}, {{event}} and {{step}} are filled in.")}>
            <MarkdownArea className="field__input" rows={5} value={cfg.body ?? ""} onChange={(e) => onChange({ ...cfg, body: e.target.value })} />
          </Field>
          <EmailStepPreview subject={cfg.subject ?? ""} body={cfg.body ?? ""} />
        </>
      )}
      {cfg.kind === "webhook" && <WebhookForm cfg={cfg} onChange={onChange} />}
      {cfg.kind === "theme" && (
        <Field label={t.t("Theme")} hint={t.t("Stays until another step changes it.")}>
          <select className="field__input" value={cfg.theme ?? ""} onChange={(e) => onChange(withOptional({ ...cfg }, "theme", e.target.value || undefined))}>
            <option value="" disabled>{t.t("— pick one —")}</option>
            <option value="default">{t.t("The adventure's own look")}</option>
            {saved.length > 0 && (
              <optgroup label={t.t("Your themes")}>
                {saved.map((th) => (
                  <option key={th.slug} value={th.slug}>{th.name || th.slug}</option>
                ))}
              </optgroup>
            )}
            <optgroup label={t.t("Built in")}>
              {Object.keys(THEMES).map((k) => (
                <option key={k} value={k}>{({ dark: t.t("Dark"), night: t.t("Night"), alarm: t.t("Alarm"), parchment: t.t("Parchment") } as Record<string, string>)[k] ?? k}</option>
              ))}
            </optgroup>
            {cfg.theme && cfg.theme !== "default" && !THEMES[cfg.theme] && !saved.some((th) => th.slug === cfg.theme) && <option value={cfg.theme}>{cfg.theme}</option>}
          </select>
        </Field>
      )}
      {cfg.kind === "theme" && openThemes && (
        <button type="button" className="btn btn--ghost btn--sm" onClick={() => openThemes(saved.some((th) => th.slug === cfg.theme) ? cfg.theme : undefined)}>
          {saved.some((th) => th.slug === cfg.theme) ? t.t("Edit this theme…") : t.t("Make or edit themes…")}
        </button>
      )}
    </>
  );
}

/**
 * How the email a step sends will reach a team: in the organisation's
 * look, with a team's and an event's names filled in as samples. Each
 * event can send in a theme of its own; this shows the organisation's.
 */
function EmailStepPreview({ subject, body }: { subject: string; body: string }) {
  const t = useT();
  const session = useSession();
  const [open, setOpen] = useState(false);
  const [preview, setPreview] = useState<emails.Preview | null>(null);
  useEffect(() => {
    if (!open) return;
    const ac = new AbortController();
    emails
      .previewDraft({ kind: "game_email", subject: subject || t.t("Your subject"), body: body || t.t("Your message to {{team}}."), step: t.t("this step") }, ac.signal)
      .then(setPreview)
      .catch(() => {});
    return () => ac.abort();
  }, [open, subject, body, t]);
  const user = session.state.status === "authenticated" ? session.state.user : null;
  const from = user?.memberships.find((m) => m.org_id === user.org_id)?.org_name ?? siteName();
  return (
    <>
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
        <Eye size={ICON.sm} />
        {t.t("Preview the email")}
      </Button>
      {open && (
        <Dialog wide title={t.t("Preview the email")} onClose={() => setOpen(false)} footer={<Button onClick={() => setOpen(false)}>{t.t("Close")}</Button>}>
          <EmailPreview
            preview={preview}
            from={from}
            onSendTest={() => emails.sendMeTest({ kind: "game_email", subject: subject || t.t("Your subject"), body: body || t.t("Your message to {{team}}."), step: t.t("this step") })}
          />
        </Dialog>
      )}
    </>
  );
}

// --- small building blocks ---------------------------------------------------


/**
 * A combination lock: how many wheels, the symbols they turn through, and
 * the right combination — written as the first route's answer, which is
 * all the lock is: a way of typing it.
 */
/** Item slots: what each is called, the item it wants, and whether the team keeps them. */
function ItemsForm({ graph, slots, onChange }: { graph: Graph; slots: AnswerOf<"items">; onChange: (a: AnswerOf<"items">) => void }) {
  const t = useT();
  const items = graph.items ?? [];
  const setSlot = (i: number, sl: ItemSlot) => onChange({ ...slots, slots: slots.slots.map((x, j) => (j === i ? sl : x)) });
  return (
    <>
      {items.length === 0 && <p className="field__hint">{t.t("This adventure has no items yet. Add them in Adventure settings, and give them to the team with a Give an item step.")}</p>}
      <Field label={t.t("Slots")} hint={t.t("In order: the team must put each slot's item in that slot.")}>
        {slots.slots.map((sl, i) => (
          <div key={i} className="box">
            <div className="box__row">
              <input
                className="field__input"
                value={sl.label ?? ""}
                placeholder={t.t("Slot {{n}}", { n: i + 1 })}
                aria-label={t.t("What the slot is called")}
                onChange={(e) => setSlot(i, withOptional({ ...sl }, "label", e.target.value || undefined))}
              />
              <button type="button" className="icon-remove" disabled={slots.slots.length <= 1} onClick={() => onChange({ ...slots, slots: slots.slots.filter((_, j) => j !== i) })} aria-label={t.t("Remove")}>
                <X size={ICON.sm} />
              </button>
            </div>
            <ResourceSelect label={t.t("Wants")} value={sl.item} options={items} onChange={(item) => setSlot(i, { ...sl, item })} />
          </div>
        ))}
        {slots.slots.length < MAX_SLOTS && (
          <button type="button" className="add-btn" onClick={() => onChange({ ...slots, slots: [...slots.slots, { item: "" }] })}>
            <Plus size={ICON.sm} aria-hidden="true" />
            {t.t("Add slot")}
          </button>
        )}
      </Field>
      <Field label={t.t("Tell a team without the items")} hint={t.t("Naming them gives part of the answer away. For a way out, add a route that fires when the team is not carrying the items.")}>
        <select className="field__input" value={slots.missing ?? ""} onChange={(e) => onChange(withOptional({ ...slots }, "missing", (e.target.value || undefined) as MissingHint | undefined))}>
          {(["", "name", "none"] as const).map((k) => (
            <option key={k} value={k}>
              {t.t(MISSING_LABEL[k])}
            </option>
          ))}
        </select>
      </Field>
      <label className="check">
        <input type="checkbox" checked={!slots.keep} onChange={(e) => onChange(withOptional({ ...slots }, "keep", e.target.checked ? undefined : true))} />
        <span>
          {t.t("Use the items up")}
          <span className="field__hint check__hint">{t.t("They leave the backpack once the slots are filled right. Off, the team keeps them — for a badge it only has to show.")}</span>
        </span>
      </label>
    </>
  );
}

function LockForm({ cfg, lock, onChange }: { cfg: PuzzleConfig; lock: AnswerOf<"lock">; onChange: (c: PuzzleConfig) => void }) {
  const t = useT();
  const outputs = cfg.outputs ?? [];
  const first = outputs.findIndex((o) => o.trigger === "answer");
  const combo = (first >= 0 ? outputs[first]!.matchers?.[0]?.value ?? "" : "").split("|");
  const setLock = (l: AnswerOf<"lock">, nextCombo = combo) => {
    const value = Array.from({ length: l.wheels }, (_, i) => nextCombo[i] ?? l.symbols[0]?.value ?? "").join("|");
    onChange({
      ...cfg,
      answer: l,
      outputs: outputs.map((o, i) => (i === first ? { ...o, matchers: [{ kind: "exact", value }] } : o)),
    });
  };
  const setSymbol = (i: number, s: LockSymbol) => {
    const old = lock.symbols[i]!.value;
    setLock({ ...lock, symbols: lock.symbols.map((x, j) => (j === i ? s : x)) }, combo.map((c) => (c === old ? s.value : c)));
  };

  return (
    <>
      <div className="box__row">
        <NumberField label={t.t("Wheels")} value={lock.wheels} onChange={(n) => setLock({ ...lock, wheels: Math.min(6, Math.max(1, Math.round(n ?? 1))) })} />
      </div>
      <Field label={t.t("Symbols")} hint={t.t("Words, or pictures — each wheel shows the same ones, in this order.")}>
        {lock.symbols.map((s, i) => (
          <div key={i} className="box">
            <div className="box__row">
              <input className="field__input" value={s.label ?? s.value} aria-label={t.t("Symbol")}
                onChange={(e) => setSymbol(i, { ...s, label: e.target.value })} />
              <button type="button" className="icon-remove" disabled={lock.symbols.length <= 2}
                onClick={() => setLock({ ...lock, symbols: lock.symbols.filter((_, j) => j !== i), start: (lock.start ?? []).map((x) => Math.min(x, lock.symbols.length - 2)) })}
                aria-label={t.t("Remove")}
              >
                <X size={ICON.sm} />
              </button>
            </div>
            <MediaEditor single media={s.image ? [s.image] : undefined} onChange={(m) => setSymbol(i, m?.[0] ? { ...s, image: m[0] } : withOptional({ ...s }, "image", undefined))} />
          </div>
        ))}
        <button type="button" className="add-btn" onClick={() => {
          let v = String(lock.symbols.length + 1);
          for (let n = lock.symbols.length + 2; lock.symbols.some((x) => x.value === v); n++) v = String(n);
          setLock({ ...lock, symbols: [...lock.symbols, { value: v, label: v }] });
        }}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add symbol")}
        </button>
      </Field>
      <Field label={t.t("The right combination")}>
        <div className="box__row">
          {Array.from({ length: lock.wheels }, (_, w) => (
            <select key={w} className="field__input" value={combo[w] ?? ""} aria-label={t.t("Wheel {{n}}", { n: w + 1 })}
              onChange={(e) => setLock(lock, Array.from({ length: lock.wheels }, (_, i) => (i === w ? e.target.value : combo[i] ?? "")))}>
              {lock.symbols.map((s) => (
                <option key={s.value} value={s.value}>{s.label || s.value}</option>
              ))}
            </select>
          ))}
        </div>
      </Field>
      <Field label={t.t("Where the wheels start")} hint={t.t("So the lock does not open at the right combination.")}>
        <div className="box__row">
          {Array.from({ length: lock.wheels }, (_, w) => (
            <select key={w} className="field__input" value={lock.start?.[w] ?? 0} aria-label={t.t("Wheel {{n}}", { n: w + 1 })}
              onChange={(e) => setLock({ ...lock, start: Array.from({ length: lock.wheels }, (_, i) => (i === w ? Number(e.target.value) : lock.start?.[i] ?? 0)) })}>
              {lock.symbols.map((s, i) => (
                <option key={i} value={i}>{s.label || s.value}</option>
              ))}
            </select>
          ))}
        </div>
      </Field>
    </>
  );
}

/**
 * Questions with options to tap. With a right answer on every question it
 * is a marked form, like an accusation; without, the option picked is the
 * answer the routes below match — "Route each option its own way" writes
 * those routes.
 */
function ChoicesForm({ cfg, choice: choices, onChange }: { cfg: PuzzleConfig; choice: AnswerOf<"choice">; onChange: (c: PuzzleConfig) => void }) {
  const t = useT();
  const graded = isGraded(choices);
  const setFields = (fields: ChoiceField[]) => onChange({ ...cfg, answer: { kind: "choice", fields } });
  const setField = (i: number, f: ChoiceField) => setFields(choices.fields.map((x, j) => (j === i ? f : x)));

  return (
    <>
      {choices.fields.map((f, i) => (
        <div key={i} className="box">
          <div className="box__row">
            <input className="field__input" value={f.label ?? ""} placeholder={t.t("Question (optional for one)")} aria-label={t.t("Question")}
              onChange={(e) => setField(i, e.target.value ? { ...f, label: e.target.value } : withOptional({ ...f }, "label", undefined))} />
            <button type="button" className="icon-remove" disabled={choices.fields.length <= 1} onClick={() => setFields(choices.fields.filter((_, j) => j !== i))} aria-label={t.t("Remove")} title={t.t("Remove")}>
              <Trash2 size={ICON.sm} />
            </button>
          </div>
          {f.options.map((o, k) => (
            <div key={k} className="box__row">
              {graded && (
                <input type="radio" name={`answer-${i}`} checked={f.answer === o.value} onChange={() => setField(i, { ...f, answer: o.value })} aria-label={t.t("Right answer")} title={t.t("Right answer")} />
              )}
              <input className="field__input" value={o.label ?? o.value} aria-label={t.t("Option")}
                onChange={(e) => setField(i, { ...f, options: f.options.map((x, j) => (j === k ? { ...x, label: e.target.value } : x)) })} />
              <button type="button" className="icon-remove" disabled={f.options.length <= 2}
                onClick={() => setField(i, withOptional({ ...f, options: f.options.filter((_, j) => j !== k) }, "answer", f.answer === o.value ? undefined : f.answer))}
                aria-label={t.t("Remove")}
              >
                <X size={ICON.sm} />
              </button>
            </div>
          ))}
          <button type="button" className="add-btn" onClick={() => {
            const label = t.t("Option {{n}}", { n: f.options.length + 1 });
            setField(i, { ...f, options: [...f.options, { value: optionValue(label, f.options), label }] });
          }}>
            <Plus size={ICON.sm} aria-hidden="true" />
            {t.t("Add option")}
          </button>
        </div>
      ))}
      <div className="box__row box__row--wrap">
        <button type="button" className="add-btn" onClick={() => setFields([...choices.fields, { label: "", options: [{ value: "a", label: "A" }, { value: "b", label: "B" }], ...(graded ? { answer: "a" } : {}) }])}>
          <Plus size={ICON.sm} aria-hidden="true" />
          {t.t("Add question")}
        </button>
        <label className="check">
          <input type="checkbox" checked={graded} onChange={(e) =>
            setFields(choices.fields.map((f) => (e.target.checked ? { ...f, answer: f.answer || f.options[0]!.value } : withOptional({ ...f }, "answer", undefined))))} />
          {t.t("Mark it: every question has a right answer")}
        </label>
        {!graded && choices.fields.length === 1 && (
          <button type="button" className="link-btn" onClick={() => onChange({
            ...cfg,
            outputs: [
              ...choices.fields[0]!.options.map((o): Output => ({ port: o.value, label: o.label || o.value, trigger: "answer", matchers: [{ kind: "exact", value: o.value }] })),
              ...(cfg.outputs ?? []).filter((o) => o.trigger !== "answer"),
            ],
          })}>
            {t.t("Route each option its own way")}
          </button>
        )}
      </div>
    </>
  );
}

/**
 * The answer bar of a typed or scanned answer: its words, and for typing
 * the keyboard. Empty fields are the phone's own.
 */
function BarForm({ cfg, onChange, scan }: { cfg: PuzzleConfig; onChange: (c: PuzzleConfig) => void; scan?: boolean }) {
  const t = useT();
  const bar = cfg.bar ?? {};
  const set = (field: keyof AnswerBar, value: AnswerBar[keyof AnswerBar] | undefined) => {
    const next = withOptional({ ...bar }, field, value || undefined);
    onChange(withOptional({ ...cfg }, "bar", Object.keys(next).length ? next : undefined));
  };
  return (
    <>
      {!scan && (
        <Field label={t.t("Placeholder")} hint={t.t("Greyed-out text in the empty box, such as “The ship's name”.")}>
          <input className="field__input" value={bar.placeholder ?? ""} placeholder={t.t("Your answer")} onChange={(e) => set("placeholder", e.target.value)} />
        </Field>
      )}
      <Field label={t.t("Button")}>
        <input className="field__input" value={bar.button ?? ""} placeholder={scan ? t.t("Scan the code") : t.t("Answer")} onChange={(e) => set("button", e.target.value)} />
      </Field>
      {!scan && (
        <Field label={t.t("Keyboard")}>
          <select className="field__input" value={bar.keyboard ?? ""} onChange={(e) => set("keyboard", e.target.value as AnswerBar["keyboard"])}>
            {(Object.keys(KEYBOARD_LABEL) as (keyof typeof KEYBOARD_LABEL)[]).map((k) => (
              <option key={k} value={k}>
                {t.t(KEYBOARD_LABEL[k])}
              </option>
            ))}
          </select>
        </Field>
      )}
    </>
  );
}

/**
 * A search: the team presses "Search here" and the route whose zone it
 * stands in is taken. The group decides which zones the phone names —
 * "You are in sector C3" — including cells with nothing to find.
 */
function SearchForm({ cfg, search, graph, onChange }: { cfg: PuzzleConfig; search: AnswerOf<"search">; graph: Graph; onChange: (c: PuzzleConfig) => void }) {
  const t = useT();
  const groups = zoneGroups(graph.zones ?? []);
  return (
    <>
      <Field label={t.t("Tell the team which of these it is in")} hint={groups.length ? undefined : t.t("Give zones a group under Adventure settings → Zones, such as the cells of a grid.")}>
        <select
          className="field__input"
          value={search.group ?? ""}
          onChange={(e) => onChange({ ...cfg, answer: e.target.value ? { kind: "search", group: e.target.value } : { kind: "search" } })}
        >
          <option value="">{t.t("The zones of the routes below")}</option>
          {groups.map((g) => (
            <option key={g} value={g}>
              {g}
            </option>
          ))}
        </select>
      </Field>
      <label className="check">
        <input type="checkbox" checked={!!cfg.repeatable} onChange={(e) => onChange(withOptional({ ...cfg }, "repeatable", e.target.checked || undefined))} />
        {t.t("Can be searched again on a later visit")}
      </label>
    </>
  );
}

/**
 * A tile puzzle's picture and grid. The phone cuts the picture itself, so
 * one upload is all it takes; the grid decides how hard it is.
 */
function TilesForm({ tiles, onChange }: { tiles: AnswerOf<"tiles">; onChange: (t: AnswerOf<"tiles">) => void }) {
  const t = useT();
  const clamp = (n: number | undefined) => Math.min(8, Math.max(2, Math.round(n ?? 3)));
  return (
    <>
      <MediaEditor
        single
        media={tiles.image ? [tiles.image] : undefined}
        onChange={(m) => onChange(m?.[0] ? { ...tiles, image: m[0] } : withOptional({ ...tiles }, "image", undefined))}
      />
      <div className="box__row">
        <NumberField label={t.t("Rows")} value={tiles.rows} onChange={(n) => onChange({ ...tiles, rows: clamp(n) })} />
        <NumberField label={t.t("Columns")} value={tiles.cols} onChange={(n) => onChange({ ...tiles, cols: clamp(n) })} />
      </div>
      <p className="field__hint">{t.t("{{n}} pieces. 3 × 3 is quick; 4 × 5 takes a few minutes.", { n: tiles.rows * tiles.cols })}</p>
    </>
  );
}

/**
 * Journal notes the team gains on reaching the step — a clue written down
 * as it is read. The notes themselves are written under Adventure
 * settings → Journal.
 */
function UnlocksField({ graph, value, onChange }: { graph: Graph; value: string[] | undefined; onChange: (v: string[]) => void }) {
  const t = useT();
  const entries = graph.journal ?? [];
  const picked = value ?? [];
  const titleOf = (slug: string) => entries.find((e) => e.slug === slug)?.title || slug;
  return (
    <Field
      label={t.t("Notes for the journal")}
      hint={entries.length ? t.t("Added to the team's journal when it reaches this step.") : t.t("Write notes under Adventure settings → Journal, then pick them here.")}
    >
      {picked.length > 0 && (
        <div className="chips">
          {picked.map((s) => (
            <span key={s} className="chip concept--journal">
              {titleOf(s)}
              <button type="button" className="chip__x" aria-label={t.t("Remove")} onClick={() => onChange(picked.filter((x) => x !== s))}>
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      {entries.some((e) => !picked.includes(e.slug)) && (
        <select className="field__input" value="" onChange={(e) => e.target.value && onChange([...picked, e.target.value])} aria-label={t.t("Add a note")}>
          <option value="">{t.t("+ Add a note…")}</option>
          {entries
            .filter((e) => !picked.includes(e.slug))
            .map((e) => (
              <option key={e.slug} value={e.slug}>
                {e.title || e.slug}
              </option>
            ))}
        </select>
      )}
    </Field>
  );
}

function NumberField({ label, value, onChange }: { label: string; value: number | undefined; onChange: (n: number | undefined) => void }) {
  return (
    <label className="field field--num">
      <span className="field__label">{label}</span>
      <input
        className="field__input"
        type="number"
        value={value ?? ""}
        onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
      />
    </label>
  );
}


function ResourceSelect({ label, value, options, onChange }: { label: string; value: string | undefined; options: { slug: string; name?: string }[]; onChange: (s: string) => void }) {
  const t = useT();
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      <select className="field__input" value={value ?? ""} onChange={(e) => onChange(e.target.value)}>
        <option value="">{t.t("— pick one —")}</option>
        {options.map((o) => (
          <option key={o.slug} value={o.slug}>
            {o.name || o.slug}
          </option>
        ))}
      </select>
    </label>
  );
}

function ListEditor<T>({
  items,
  make,
  onChange,
  render,
  addLabel,
}: {
  items: T[];
  make: () => T;
  onChange: (items: T[]) => void;
  render: (item: T, set: (next: T) => void) => ReactNode;
  addLabel: string;
}) {
  const t = useT();
  return (
    <div className="list-ed">
      {items.map((item, i) => (
        <div key={i} className="list-ed__item">
          <div className="list-ed__body">{render(item, (next) => onChange(items.map((x, j) => (j === i ? next : x))))}</div>
          <button type="button" className="icon-remove" onClick={() => onChange(items.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <button type="button" className="add-btn" onClick={() => onChange([...items, make()])}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {addLabel}
      </button>
    </div>
  );
}

/**
 * Set or clear an optional field. With exactOptionalPropertyTypes a field
 * cannot be assigned `undefined`; it has to be absent. Absent also keeps
 * the saved JSON clean, which matters because the server rejects nothing
 * for an absent field but a `null` would be a type error on its side.
 */
/** "Mark where I stand" for a puzzle: the new zone and the puzzle's gate on it, in one edit. */
function puzzleHere(node: Node, graph: Graph, onGraphChange: (g: Graph) => void, t: T): ZoneHere {
  return {
    name: node.title || t.t("Puzzle"),
    set: (zones, slug) => onGraphChange(updateNode({ ...graph, zones }, node.slug, (n) => ({ ...n, puzzle: { ...n.puzzle!, zone: slug } }))),
  };
}

function withOptional<T extends object, K extends keyof T>(obj: T, key: K, value: T[K] | undefined): T {
  const out = { ...obj };
  if (value === undefined || (typeof value === "number" && Number.isNaN(value))) delete out[key];
  else out[key] = value;
  return out;
}

/**
 * A webhook: where, how, and with which headers. The body is the team and
 * step as JSON; GET and DELETE carry them in the query instead.
 */
function WebhookForm({ cfg, onChange }: { cfg: EffectConfig; onChange: (c: EffectConfig) => void }) {
  const t = useT();
  const headers = cfg.headers ?? [];
  const setHeaders = (next: { name: string; value: string }[]) => onChange(withOptional({ ...cfg }, "headers", next.length ? next : undefined));
  const bodyless = cfg.method === "GET" || cfg.method === "DELETE";
  return (
    <>
      <div className="box__row">
        <label className="field webhook__method">
          <span className="field__label">{t.t("Method")}</span>
          <select className="field__input" value={cfg.method ?? "POST"} onChange={(e) => onChange(withOptional({ ...cfg }, "method", e.target.value === "POST" ? undefined : (e.target.value as WebhookMethod)))}>
            {WEBHOOK_METHODS.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </label>
        <label className="field" style={{ flex: 1, minWidth: 0 }}>
          <span className="field__label">{t.t("URL")}</span>
          <input className="field__input" type="url" value={cfg.url ?? ""} placeholder="https://" onChange={(e) => onChange({ ...cfg, url: e.target.value })} />
        </label>
      </div>
      <span className="field__label">{t.t("Headers")}</span>
      {headers.map((h, i) => (
        <div key={i} className="box__row webhook__header">
          <input
            className="field__input"
            value={h.name}
            placeholder={t.t("Name, such as Authorization")}
            aria-label={t.t("Header name")}
            onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))}
          />
          <input
            className="field__input"
            value={h.value}
            placeholder={t.t("Value")}
            aria-label={t.t("Header value")}
            onChange={(e) => setHeaders(headers.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))}
          />
          <button type="button" className="icon-remove" onClick={() => setHeaders(headers.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <button type="button" className="add-btn concept--effect" onClick={() => setHeaders([...headers, { name: "", value: "" }])}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {t.t("Add header")}
      </button>
      <p className="field__hint">
        {bodyless
          ? t.t("Sent once per team as it passes, with event_id, team_id, team, node and queued_at in the query. Retried for a day if the server fails.")
          : t.t("Sent once per team as it passes, with event_id, team_id, team, node and queued_at as JSON. Retried for a day if the server fails.")}
      </p>
      {headers.length > 0 && (
        <p className="field__hint">{t.t("Header values are saved in the adventure: anyone who can open or export it can read them. Use a key meant for this, not a personal password.")}</p>
      )}
    </>
  );
}
