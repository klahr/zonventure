import type { DividerStyle, NoteStyle } from "@zonventure/graph";
import type { Editor } from "./types.ts";

const NOTE_STYLES: { value: NoteStyle; label: string }[] = [
  { value: "paper", label: "Paper note" },
  { value: "telegram", label: "Telegram" },
  { value: "sms", label: "Text message" },
  { value: "newspaper", label: "Newspaper clipping" },
  { value: "sticky", label: "Sticky note" },
  { value: "mirror", label: "Mirror writing" },
];

export const NoteEditor: Editor<"note"> = ({ block, set, t }) => (
  <>
    <select className="field__input" value={block.style ?? "paper"} aria-label={t.t("Style")} onChange={(e) => set({ ...block, style: e.target.value as NoteStyle })}>
      {NOTE_STYLES.map((s) => (
        <option key={s.value} value={s.value}>
          {t.t(s.label)}
        </option>
      ))}
    </select>
    <textarea className="field__input" rows={4} value={block.text} placeholder={t.t("Markdown.")} aria-label={t.t("Note")} onChange={(e) => set({ ...block, text: e.target.value })} />
  </>
);

const DIVIDER_STYLES: { value: DividerStyle; label: string }[] = [
  { value: "line", label: "Line" },
  { value: "space", label: "Space" },
  { value: "ornament", label: "Ornament" },
];

export const DividerEditor: Editor<"divider"> = ({ block, set, t }) => (
  <select className="field__input" value={block.style ?? "line"} aria-label={t.t("Style")} onChange={(e) => set({ ...block, style: e.target.value as DividerStyle })}>
    {DIVIDER_STYLES.map((s) => (
      <option key={s.value} value={s.value}>
        {t.t(s.label)}
      </option>
    ))}
  </select>
);
