import type { ChatLine } from "@zonventure/graph";
import { Plus, X } from "lucide-react";
import { ICON } from "../dz/ui.tsx";
import { MediaEditor } from "../MediaEditor.tsx";
import { withField, type Editor } from "./types.ts";
import "./chat-editor.css";

/** A chat's avatar and messages, in order; a chat keeps at least one. */
export const ChatEditor: Editor<"chat"> = ({ block, set, t }) => {
  const lines = block.lines;
  const setLines = (next: ChatLine[]) => set({ ...block, lines: next });
  const setLine = (i: number, l: ChatLine) => setLines(lines.map((x, j) => (j === i ? l : x)));
  const avatar = block.media ? [{ id: block.media, kind: "image" as const, ...(block.alt ? { alt: block.alt } : {}) }] : undefined;
  return (
    <div className="list-ed">
      <div className="blk-chat__avatar">
        <span className="field__label">{t.t("Avatar")}</span>
        <MediaEditor
          media={avatar}
          single
          onChange={(m) => set(withField(withField({ ...block }, "media", m?.[0]?.id), "alt", m?.[0]?.alt || undefined))}
        />
      </div>
      {lines.map((l, i) => (
        <div key={i} className={"list-ed__item blk-chat__line" + (l.me ? " is-me" : "")}>
          <div className="list-ed__body">
            <div className="blk-chat__head">
              <input className="field__input" value={l.who ?? ""} placeholder={t.t("Who")} aria-label={t.t("Who")} onChange={(e) => setLine(i, withField(l, "who", e.target.value))} />
              <label className="check">
                <input type="checkbox" checked={!!l.me} onChange={(e) => setLine(i, withField(l, "me", e.target.checked))} />
                <span>{t.t("From the team")}</span>
              </label>
            </div>
            <textarea className="field__input" rows={2} value={l.text} placeholder={t.t("Message")} aria-label={t.t("Message {{n}}", { n: i + 1 })} onChange={(e) => setLine(i, { ...l, text: e.target.value })} />
          </div>
          <button type="button" className="icon-remove" disabled={lines.length < 2} onClick={() => setLines(lines.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </button>
        </div>
      ))}
      <button type="button" className="add-btn" onClick={() => setLines([...lines, { text: "" }])}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {t.t("Add message")}
      </button>
      <p className="field__hint">{t.t("The team sees one message at a time, as if typed, and what follows the chat once it is over. The avatar heads the other side's messages. {{team}} is the team's name.", { team: "{{team}}" })}</p>
    </div>
  );
};
