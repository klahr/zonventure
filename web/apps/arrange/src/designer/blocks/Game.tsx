import type { Editor } from "./types.ts";
import { withField } from "./types.ts";

export const CountdownEditor: Editor<"countdown"> = ({ block, set, t }) => (
  <>
    <input className="field__input" value={block.text ?? ""} placeholder={t.t("Label, such as “The bomb goes off in”")} aria-label={t.t("Label")} onChange={(e) => set(withField(block, "text", e.target.value))} />
    <div className="box__row">
      <label className="field field--num">
        <span className="field__label">{t.t("Minutes")}</span>
        <input
          className="field__input"
          type="number"
          min={1}
          value={Math.round(block.seconds / 60)}
          onChange={(e) => set({ ...block, seconds: Math.max(1, Number(e.target.value) || 1) * 60 })}
        />
      </label>
      <label className="field">
        <span className="field__label">{t.t("Counting from")}</span>
        <select className="field__input" value={block.from ?? "step"} onChange={(e) => set(withField(block, "from", e.target.value === "start" ? "start" : undefined))}>
          <option value="step">{t.t("reaching this step")}</option>
          <option value="start">{t.t("the team's start")}</option>
        </select>
      </label>
    </div>
  </>
);

export const StatusEditor: Editor<"status"> = ({ block, set, t, graph }) => {
  const chosen = new Set(block.counters ?? []);
  const counters = graph.counters ?? [];
  return (
    <>
      <label className="check">
        <input type="checkbox" checked={!!block.items} onChange={(e) => set(withField(block, "items", e.target.checked))} />
        <span>{t.t("What the team carries")}</span>
      </label>
      {counters.map((c) => (
        <label key={c.slug} className="check">
          <input
            type="checkbox"
            checked={chosen.has(c.slug)}
            onChange={(e) => {
              const next = counters.map((x) => x.slug).filter((s) => (s === c.slug ? e.target.checked : chosen.has(s)));
              set(withField(block, "counters", next.length ? next : undefined));
            }}
          />
          <span>{c.name || c.slug}</span>
        </label>
      ))}
      <p className="field__hint">{t.t("Texts can show values too: {{score}}, {{counter:slug}}, {{item:slug}}, {{team}}.")}</p>
    </>
  );
};

export const StandingsEditor: Editor<"standings"> = ({ t }) => <p className="field__hint">{t.t("Shows the team's place in the event, and the teams just ahead and behind.")}</p>;
