import type { ReactNode } from "react";
import type {
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
  Response,
  Trigger,
} from "@zonventure/graph";
import { useT } from "../i18n.tsx";
import { ConditionEditor } from "./ConditionEditor.tsx";

interface Props {
  node: Node;
  graph: Graph;
  issues: Issue[];
  onChange: (n: Node) => void;
  onDelete: () => void;
}

/**
 * The right-hand panel: every setting of the selected node.
 *
 * Every field writes straight through to the document; the undo history
 * coalesces consecutive edits to one node, so typing a prompt is one undo
 * step rather than one per keystroke.
 */
export function Inspector({ node, graph, issues, onChange, onDelete }: Props) {
  const t = useT();

  return (
    <div className="insp">
      <div className="insp__head">
        <span className={`insp__type insp__type--${node.type}`}>{node.type}</span>
        <code className="insp__slug" title={t.t("The step's permanent address. It never changes, so saved team history and conditions keep pointing at it.")}>
          {node.slug}
        </code>
      </div>

      {issues.length > 0 && (
        <ul className="insp__issues">
          {issues.map((i, k) => (
            <li key={k} className={`issue issue--${i.severity}`}>
              {i.port ? <strong>{i.port}: </strong> : null}
              {i.message}
            </li>
          ))}
        </ul>
      )}

      <Field label={t.t("Title")}>
        <input className="field__input" value={node.title ?? ""} onChange={(e) => onChange({ ...node, title: e.target.value })} />
      </Field>

      {node.type === "puzzle" && node.puzzle && (
        <PuzzleForm cfg={node.puzzle} graph={graph} onChange={(puzzle) => onChange({ ...node, puzzle })} />
      )}

      {node.type === "story" && (
        <>
          <Field label={t.t("Text")} hint={t.t("Markdown.")}>
            <textarea
              className="field__input"
              rows={6}
              value={node.story?.text ?? ""}
              onChange={(e) => onChange({ ...node, story: { ...node.story, text: e.target.value } })}
            />
          </Field>
          <ZoneSelect
            graph={graph}
            label={t.t("Only continue inside a zone")}
            value={node.story?.zone}
            onChange={(zone) => onChange({ ...node, story: withOptional({ ...node.story }, "zone", zone) })}
          />
        </>
      )}

      {node.type === "hub" && node.hub && (
        <HubForm cfg={node.hub} graph={graph} onChange={(hub) => onChange({ ...node, hub })} />
      )}

      {node.type === "gate" && node.gate && (
        <Field label={t.t("Pass when")} hint={t.t("Otherwise the team takes the fail route.")}>
          <ConditionEditor graph={graph} value={node.gate.condition} onChange={(condition) => onChange({ ...node, gate: { condition } })} />
        </Field>
      )}

      {node.type === "effect" && node.effect && (
        <EffectForm cfg={node.effect} graph={graph} onChange={(effect) => onChange({ ...node, effect })} />
      )}

      {node.type === "end" && (
        <>
          <Field label={t.t("Outcome")}>
            <select
              className="field__input"
              value={node.end?.outcome ?? "complete"}
              onChange={(e) => onChange({ ...node, end: { ...node.end, outcome: e.target.value as "complete" | "fail" } })}
            >
              <option value="complete">{t.t("Success")}</option>
              <option value="fail">{t.t("Failure")}</option>
            </select>
          </Field>
          <Field label={t.t("Closing text")}>
            <textarea
              className="field__input"
              rows={4}
              value={node.end?.text ?? ""}
              onChange={(e) => onChange({ ...node, end: { ...node.end, text: e.target.value } })}
            />
          </Field>
        </>
      )}

      {node.type !== "start" && (
        <button type="button" className="btn btn--ghost insp__delete" onClick={onDelete}>
          {t.t("Delete step")}
        </button>
      )}
    </div>
  );
}

// --- puzzle ----------------------------------------------------------------

const TRIGGERS: { t: Trigger; label: string }[] = [
  { t: "answer", label: "Correct answer" },
  { t: "attempts", label: "After N wrong guesses" },
  { t: "timeout", label: "Time runs out" },
  { t: "skipped", label: "Team skips" },
];

function PuzzleForm({ cfg, graph, onChange }: { cfg: PuzzleConfig; graph: Graph; onChange: (c: PuzzleConfig) => void }) {
  const t = useT();
  const outputs = cfg.outputs ?? [];
  const setOutput = (i: number, o: Output) => onChange({ ...cfg, outputs: outputs.map((x, j) => (j === i ? o : x)) });

  const addOutput = (trigger: Trigger) => {
    const taken = new Set(outputs.map((o) => o.port));
    let port = trigger === "answer" ? "answer" : trigger;
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

  return (
    <>
      <Field label={t.t("Prompt")} hint={t.t("Markdown. This is what the team reads.")}>
        <textarea className="field__input" rows={5} value={cfg.prompt ?? ""} onChange={(e) => onChange({ ...cfg, prompt: e.target.value })} />
      </Field>

      <ZoneSelect
        graph={graph}
        label={t.t("Only answerable inside a zone")}
        value={cfg.zone}
        onChange={(zone) => onChange(withOptional({ ...cfg }, "zone", zone))}
      />

      <Section title={t.t("Ways out")} hint={t.t("Each route becomes an output you can wire somewhere different.")}>
        {outputs.map((o, i) => (
          <div key={i} className="box">
            <div className="box__row">
              <input
                className="field__input"
                value={o.label ?? ""}
                placeholder={o.port}
                onChange={(e) => setOutput(i, { ...o, label: e.target.value })}
                aria-label={t.t("Route label")}
              />
              <select className="field__input" value={o.trigger} onChange={(e) => setOutput(i, { ...o, trigger: e.target.value as Trigger })} aria-label={t.t("When")}>
                {TRIGGERS.map((x) => (
                  <option key={x.t} value={x.t}>
                    {t.t(x.label)}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="link-btn"
                disabled={outputs.length <= 1}
                title={outputs.length <= 1 ? t.t("A puzzle needs at least one way out.") : undefined}
                onClick={() => onChange({ ...cfg, outputs: outputs.filter((_, j) => j !== i) })}
              >
                {t.t("Remove")}
              </button>
            </div>

            {o.trigger === "answer" && (
              <>
                <MatcherList matchers={o.matchers ?? []} onChange={(matchers) => setOutput(i, { ...o, matchers })} />
                <div className="box__row">
                  <NumberField label={t.t("Points")} value={o.points} onChange={(points) => setOutput(i, withOptional({ ...o }, "points", points))} />
                </div>
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
              </>
            )}
            {o.trigger === "attempts" && (
              <NumberField label={t.t("After this many wrong guesses")} value={o.after_attempts} onChange={(n) => setOutput(i, withOptional({ ...o }, "after_attempts", n))} />
            )}
          </div>
        ))}
        <div className="box__row box__row--wrap">
          <button type="button" className="link-btn" onClick={() => addOutput("answer")}>+ {t.t("Another answer")}</button>
          <button type="button" className="link-btn" onClick={() => addOutput("attempts")}>+ {t.t("Pity route")}</button>
          <button type="button" className="link-btn" onClick={() => addOutput("timeout")}>+ {t.t("Timeout route")}</button>
          <button type="button" className="link-btn" onClick={() => addOutput("skipped")}>+ {t.t("Skip route")}</button>
        </div>
      </Section>

      <Section title={t.t("Wrong-answer replies")} hint={t.t("Shown after a wrong guess. Each applies from its number of wrong guesses on; the last repeats.")}>
        <ListEditor<Response>
          items={cfg.responses ?? []}
          make={() => ({ after_attempts: (cfg.responses?.length ?? 0) + 1, text: "" })}
          onChange={(responses) => onChange(withOptional({ ...cfg }, "responses", responses.length ? responses : undefined))}
          render={(r, set) => (
            <div className="box__row">
              <NumberField label={t.t("From guess")} value={r.after_attempts} onChange={(n) => set(withOptional({ ...r }, "after_attempts", n))} />
              <input className="field__input" value={r.text} placeholder={t.t("Not quite…")} onChange={(e) => set({ ...r, text: e.target.value })} aria-label={t.t("Reply")} />
            </div>
          )}
          addLabel={t.t("Add reply")}
        />
      </Section>

      <Section title={t.t("Hints")}>
        <ListEditor<Hint>
          items={cfg.hints ?? []}
          make={() => ({ text: "", delay_seconds: 120 })}
          onChange={(hints) => onChange(withOptional({ ...cfg }, "hints", hints.length ? hints : undefined))}
          render={(h, set) => (
            <>
              <textarea className="field__input" rows={2} value={h.text} onChange={(e) => set({ ...h, text: e.target.value })} aria-label={t.t("Hint")} />
              <div className="box__row">
                <NumberField label={t.t("Available after (s)")} value={h.delay_seconds} onChange={(n) => set(withOptional({ ...h }, "delay_seconds", n))} />
                <NumberField label={t.t("Costs points")} value={h.cost} onChange={(n) => set(withOptional({ ...h }, "cost", n))} />
              </div>
            </>
          )}
          addLabel={t.t("Add hint")}
        />
      </Section>

      <Section title={t.t("Rules")}>
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
      </Section>
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

function HubForm({ cfg, graph, onChange }: { cfg: HubConfig; graph: Graph; onChange: (c: HubConfig) => void }) {
  const t = useT();
  const branches = cfg.branches ?? [];
  const policy = POLICIES.find((p) => p.p === cfg.policy);

  return (
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
      {cfg.policy === "choice" && (
        <label className="check">
          <input type="checkbox" checked={!!cfg.hide_locked} onChange={(e) => onChange(withOptional({ ...cfg }, "hide_locked", e.target.checked || undefined))} />
          {t.t("Hide branches that are not open yet")}
        </label>
      )}

      <Section title={t.t("Branches")}>
        {branches.map((b, i) => (
          <div key={b.port} className="box">
            <div className="box__row">
              <input
                className="field__input"
                value={b.label ?? ""}
                placeholder={b.port}
                onChange={(e) => onChange({ ...cfg, branches: branches.map((x, j) => (j === i ? { ...b, label: e.target.value } : x)) })}
                aria-label={t.t("Branch label")}
              />
              {cfg.policy === "capacity" && (
                <NumberField
                  label={t.t("Max teams")}
                  value={b.capacity}
                  onChange={(n) => onChange({ ...cfg, branches: branches.map((x, j) => (j === i ? withOptional({ ...b }, "capacity", n) : x)) })}
                />
              )}
              <button
                type="button"
                className="link-btn"
                disabled={branches.length <= 1}
                onClick={() => onChange({ ...cfg, branches: branches.filter((_, j) => j !== i) })}
              >
                {t.t("Remove")}
              </button>
            </div>
            <details className="box__more" open={!!b.condition?.op}>
              <summary>{t.t("Only offer when…")}</summary>
              <ConditionEditor
                graph={graph}
                value={b.condition ?? { op: "" }}
                onChange={(c) =>
                  onChange({ ...cfg, branches: branches.map((x, j) => (j === i ? withOptional({ ...b }, "condition", c.op ? c : undefined) : x)) })
                }
              />
            </details>
          </div>
        ))}
        <button
          type="button"
          className="link-btn"
          onClick={() => {
            const port = nextBranchPort(branches.map((b) => b.port));
            onChange({ ...cfg, branches: [...branches, { port, label: port.toUpperCase() }] });
          }}
        >
          + {t.t("Add branch")}
        </button>
      </Section>
    </>
  );
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
        <Field label={t.t("Template")} hint={t.t("Queued with the team's move and delivered afterwards.")}>
          <input className="field__input" value={cfg.template ?? ""} onChange={(e) => onChange({ ...cfg, template: e.target.value })} />
        </Field>
      )}
      {cfg.kind === "webhook" && (
        <Field label={t.t("URL")}>
          <input className="field__input" type="url" value={cfg.url ?? ""} placeholder="https://" onChange={(e) => onChange({ ...cfg, url: e.target.value })} />
        </Field>
      )}
      {cfg.kind === "theme" && (
        <Field label={t.t("Theme name")}>
          <input className="field__input" value={cfg.theme ?? ""} onChange={(e) => onChange({ ...cfg, theme: e.target.value })} />
        </Field>
      )}
    </>
  );
}

// --- small building blocks ---------------------------------------------------

function Field({ label, hint, children }: { label: string; hint?: string | undefined; children: ReactNode }) {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      {children}
      {hint && <span className="field__hint">{hint}</span>}
    </label>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section className="insp__section">
      <h3>{title}</h3>
      {hint && <p className="field__hint">{hint}</p>}
      {children}
    </section>
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

function ZoneSelect({ graph, label, value, onChange }: { graph: Graph; label: string; value: string | undefined; onChange: (z: string | undefined) => void }) {
  const t = useT();
  const zones = graph.zones ?? [];
  return (
    <Field label={label} hint={zones.length === 0 ? t.t("Add zones in the Zones & items panel.") : undefined}>
      <select className="field__input" value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
        <option value="">{t.t("Anywhere")}</option>
        {zones.map((z) => (
          <option key={z.slug} value={z.slug}>
            {z.name || z.slug} ({z.radius_m} m)
          </option>
        ))}
      </select>
    </Field>
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
          <button type="button" className="link-btn" onClick={() => onChange(items.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            ×
          </button>
        </div>
      ))}
      <button type="button" className="link-btn" onClick={() => onChange([...items, make()])}>
        + {addLabel}
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
function withOptional<T extends object, K extends keyof T>(obj: T, key: K, value: T[K] | undefined): T {
  const out = { ...obj };
  if (value === undefined || (typeof value === "number" && Number.isNaN(value))) delete out[key];
  else out[key] = value;
  return out;
}
