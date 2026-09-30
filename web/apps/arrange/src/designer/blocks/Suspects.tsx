import type { SuspectCard } from "@zonventure/graph";
import { Plus, X } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import { MediaEditor } from "../MediaEditor.tsx";
import { withField, type Editor } from "./types.ts";
import "./chat-editor.css";
import { MarkdownArea } from "../MarkdownArea.tsx";

/** A suspect board's cards, in order; a board keeps at least one. */
export const SuspectsEditor: Editor<"suspects"> = ({ block, set, t }) => {
  const cards = block.cards ?? [];
  const setCards = (next: SuspectCard[]) => set({ ...block, cards: next });
  const setCard = (i: number, c: SuspectCard) => setCards(cards.map((x, j) => (j === i ? c : x)));
  return (
    <div className="list-ed">
      {cards.map((c, i) => (
        <div key={i} className="list-ed__item">
          <div className="list-ed__body">
            <input className="field__input" value={c.name} placeholder={t.t("Name, such as “The baker”")} aria-label={t.t("Suspect {{n}}", { n: i + 1 })} onChange={(e) => setCard(i, { ...c, name: e.target.value })} />
            <MarkdownArea className="field__input" rows={2} value={c.text ?? ""} placeholder={t.t("Markdown. A few facts about them.")} aria-label={t.t("Facts")} onChange={(e) => setCard(i, withField(c, "text", e.target.value))} />
            <div className="blk-chat__avatar">
              <span className="field__label">{t.t("Picture")}</span>
              <MediaEditor
                media={c.media ? [{ id: c.media, kind: "image", ...(c.alt ? { alt: c.alt } : {}) }] : undefined}
                single
                onChange={(m) => setCard(i, withField(withField({ ...c }, "media", m?.[0]?.id), "alt", m?.[0]?.alt || undefined))}
              />
            </div>
          </div>
          <button type="button" className="icon-remove" disabled={cards.length < 2} onClick={() => setCards(cards.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <button type="button" className="add-btn" onClick={() => setCards([...cards, { name: "" }])}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {t.t("Add suspect")}
      </button>
      <p className="field__hint">{t.t("The team taps a card to strike the suspect out, and again to bring them back. What it struck stays when it comes back to the step; it never scores.")}</p>
    </div>
  );
};
