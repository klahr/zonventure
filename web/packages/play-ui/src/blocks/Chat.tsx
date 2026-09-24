import { useEffect, useState } from "react";
import { fillTeam, type Renderer } from "./context.ts";
import "./chat.css";

/** Long enough to feel typed, short enough not to drag: 0.6–1.8 s. */
const pace = (text: string) => Math.min(1800, 600 + text.length * 25);

/**
 * An intercepted conversation, played back a message at a time with
 * someone typing in between. A designer's preview, or a phone that asks
 * for less motion, gets the whole thread at once.
 */
export const ChatBlock: Renderer<"chat"> = ({ block, ctx }) => {
  const lines = block.lines;
  const [shown, setShown] = useState(() => (ctx.preview || prefersReducedMotion() ? Infinity : 0));
  const count = Math.min(shown, lines.length);
  const next = lines[count];
  // Keyed on the wait, not the line, so a refetched block keeps its place.
  const wait = next ? pace(next.text) : 0;
  useEffect(() => {
    if (!wait) return;
    const id = setTimeout(() => setShown((n) => n + 1), wait);
    return () => clearTimeout(id);
  }, [count, wait]);
  if (!lines.length) return null;
  return (
    <div className="pv-chat">
      <div className="pv-chat__log" role="log" aria-live="polite">
        {lines.slice(0, count).map((l, i) => {
          const prev = lines[i - 1];
          // A name heads each run of messages from the same sender.
          const heads = l.who && (!prev || prev.who !== l.who || !prev.me !== !l.me);
          return (
            <div key={i} className={"pv-chat__msg" + (l.me ? " is-me" : "")}>
              {heads && <span className="pv-chat__who">{fillTeam(l.who ?? "", ctx)}</span>}
              <p className="pv-chat__bubble">{fillTeam(l.text, ctx)}</p>
            </div>
          );
        })}
        {next && (
          <div className={"pv-chat__msg" + (next.me ? " is-me" : "")}>
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
