import { useEffect, useState } from "react";
import { play } from "@zonventure/api";
import { typingTime } from "../chat.ts";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./chat.css";

/**
 * An intercepted conversation, played back a message at a time with
 * someone typing in between. The designer's step preview, or a phone
 * that asks for less motion, gets the whole thread at once. The avatar,
 * when there is one, heads each run of the other side's messages.
 */
export const ChatBlock: Renderer<"chat"> = ({ block, ctx }) => {
  const lines = block.lines;
  const [shown, setShown] = useState(() => (ctx.instant || prefersReducedMotion() ? Infinity : 0));
  const count = Math.min(shown, lines.length);
  const next = lines[count];
  // Keyed on the wait, not the line, so a refetched block keeps its place.
  const wait = next ? typingTime(next.text) : 0;
  useEffect(() => {
    if (!wait) return;
    const id = setTimeout(() => setShown((n) => n + 1), wait);
    return () => clearTimeout(id);
  }, [count, wait]);
  const { played } = ctx;
  const finished = count >= lines.length;
  useEffect(() => {
    if (finished) played?.(block.id);
  }, [finished, played, block.id]);
  if (!lines.length) return null;
  // A name, with the avatar on the other side's, heads each run of
  // messages from the same sender — and whoever is typing the next one.
  const head = (i: number) => {
    const l = lines[i]!;
    const prev = lines[i - 1];
    if (!l.who || (prev && prev.who === l.who && !prev.me === !l.me)) return null;
    return (
      <span className="pv-chat__who">
        {block.media && !l.me ? <img className="pv-chat__avatar" src={play.mediaURL(block.media)} alt="" /> : null}
        {fillTeam(l.who, ctx)}
      </span>
    );
  };
  return (
    <div className="pv-chat">
      <div className="pv-chat__log" role="log" aria-live="polite">
        {lines.slice(0, count).map((l, i) => (
          <div key={i} className={"pv-chat__msg" + (l.me ? " is-me" : "")}>
            {head(i)}
            <Markdown className="pv-chat__bubble" text={fillTeam(l.text, ctx)} />
          </div>
        ))}
        {next && (
          <div className={"pv-chat__msg" + (next.me ? " is-me" : "")}>
            {head(count)}
            <span className="pv-chat__typing" role="img" aria-label={ctx.t("Typing…")}>
              <i />
              <i />
              <i />
            </span>
          </div>
        )}
      </div>
      {next && (
        <button type="button" className="pv-chat__all" onClick={() => setShown(Infinity)}>
          {ctx.t("Show all")}
        </button>
      )}
    </div>
  );
};

function prefersReducedMotion(): boolean {
  return typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
}
