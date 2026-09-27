import { Plus, X } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import { withField, type Editor, type T } from "./types.ts";

/** A list of texts that keeps at least `min`: a split secret's parts, a vote's options, a draw's cards. */
function TextList({ items, set, min, rows, placeholder, label, addLabel, t }: { items: string[]; set: (next: string[]) => void; min: number; rows: number; placeholder: string; label: (i: number) => string; addLabel: string; t: T }) {
  const change = (i: number, v: string) => set(items.map((x, j) => (j === i ? v : x)));
  return (
    <div className="list-ed">
      {items.map((s, i) => (
        <div key={i} className="list-ed__item">
          <div className="list-ed__body">
            {rows > 1 ? (
              <textarea className="field__input" rows={rows} value={s} placeholder={placeholder} aria-label={label(i)} onChange={(e) => change(i, e.target.value)} />
            ) : (
              <input className="field__input" value={s} placeholder={placeholder} aria-label={label(i)} onChange={(e) => change(i, e.target.value)} />
            )}
          </div>
          <button type="button" className="icon-remove" disabled={items.length <= min} onClick={() => set(items.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <button type="button" className="add-btn" onClick={() => set([...items, ""])}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {addLabel}
      </button>
    </div>
  );
}

export const SplitEditor: Editor<"split"> = ({ block, set, t }) => (
  <>
    <TextList items={block.parts ?? []} set={(parts) => set({ ...block, parts })} min={2} rows={2} placeholder={t.t("Markdown. One piece of the secret.")} label={(i) => t.t("Part {{n}}", { n: i + 1 })} addLabel={t.t("Add part")} t={t} />
    <p className="field__hint">{t.t("Each player's phone shows one part; with fewer players than parts some get several, and a player alone sees all.")}</p>
  </>
);

export const VoteEditor: Editor<"vote"> = ({ block, set, t }) => (
  <>
    <textarea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("The question, such as “Which door do we open?”")} aria-label={t.t("Question")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <TextList items={block.options ?? []} set={(options) => set({ ...block, options })} min={2} rows={1} placeholder={t.t("An option")} label={(i) => t.t("Option {{n}}", { n: i + 1 })} addLabel={t.t("Add option")} t={t} />
    <p className="field__hint">{t.t("Each player votes from their own phone; everyone sees the tally. A vote never routes.")}</p>
  </>
);

export const DrawEditor: Editor<"draw"> = ({ block, set, t }) => (
  <>
    <textarea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("Markdown. Such as “Draw your fortune.”")} aria-label={t.t("Prompt")} onChange={(e) => set(withField(block, "text", e.target.value))} />
    <TextList items={block.options ?? []} set={(options) => set({ ...block, options })} min={2} rows={1} placeholder={t.t("A card")} label={(i) => t.t("Card {{n}}", { n: i + 1 })} addLabel={t.t("Add card")} t={t} />
    <p className="field__hint">{t.t("The team draws one card at random, once. A show condition can ask which card was drawn.")}</p>
  </>
);

export const PhotoEditor: Editor<"photo"> = ({ block, set, t }) => (
  <>
    <textarea
      className="field__input"
      rows={2}
      value={block.text ?? ""}
      placeholder={t.t("Such as “Take a picture of your team with the statue in the background.”")}
      aria-label={t.t("What to photograph")}
      onChange={(e) => set({ ...block, text: e.target.value })}
    />
    <label className="field field--num">
      <span className="field__label">{t.t("Points when approved")}</span>
      <input className="field__input" type="number" min={0} value={block.worth ?? 0} onChange={(e) => set(withField(block, "worth", Math.max(0, Math.round(Number(e.target.value)) || 0) || undefined))} />
    </label>
    <p className="field__hint">
      {t.t("The arranger approves or denies each picture from the event's control page. An approved one adds the points; after a denied one the team can take a new picture. The team never waits for the review.")}
    </p>
  </>
);

export const BulletinEditor: Editor<"bulletin"> = ({ t }) => <p className="field__hint">{t.t("Shows the newest message the organisers sent this team from the event's control page.")}</p>;
