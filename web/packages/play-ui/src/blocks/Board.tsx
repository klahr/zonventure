import { useEffect, useRef, useState } from "react";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/** The organisers' newest message, pinned to the step like a notice. */
export const BulletinBlock: Renderer<"bulletin"> = ({ ctx }) => {
  const { t } = ctx;
  const b = ctx.bulletin;
  const at = b ? new Date(b.created_at) : null;
  return (
    <section className="pv-bulletin" aria-label={t("News from the organisers")} aria-live="polite">
      <span className="pv-bulletin__pin" aria-hidden="true" />
      <span className="pv-bulletin__tag">{t("News from the organisers")}</span>
      {b ? (
        <>
          <p className="pv-bulletin__text">{b.text}</p>
          {at && !Number.isNaN(at.getTime()) && <time className="pv-bulletin__at" dateTime={b.created_at}>{at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</time>}
        </>
      ) : (
        <p className="pv-bulletin__text pv-muted">{t("No news from the organisers yet.")}</p>
      )}
    </section>
  );
};

/**
 * A face-down deck: the team draws one card, dealt at random by the
 * server and kept, so every phone sees the same one. The cards stay out
 * of the view until drawn.
 */
export const DrawBlock: Renderer<"draw"> = ({ block, ctx }) => {
  const { t } = ctx;
  const drawn = block.dealt != null && block.picked != null && block.picked !== "";
  // Flip only a card drawn while watching, not one already on the table.
  const wasDrawn = useRef(drawn);
  const [flip, setFlip] = useState(false);
  useEffect(() => {
    if (drawn && !wasDrawn.current) setFlip(true);
    wasDrawn.current = drawn;
  }, [drawn]);
  const draw = () => ctx.node && ctx.act?.({ kind: "pick", node: ctx.node, block: block.id, choice: "" });
  return (
    <section className="pv-draw" aria-label={t("Draw a card")}>
      {block.text && <Markdown className="pv-draw__q" text={fillTeam(block.text, ctx)} />}
      <div className="pv-draw__table">
        {drawn ? (
          <div className={"pv-draw__card is-up" + (flip ? " is-flipping" : "")} role="status">
            <span className="pv-draw__face">{block.dealt}</span>
          </div>
        ) : (
          <button type="button" className="pv-draw__card pv-draw__deck" disabled={ctx.busy || !ctx.act} onClick={draw} aria-label={t("Draw a card")}>
            <span className="pv-draw__back" aria-hidden="true" />
          </button>
        )}
      </div>
      <p className="pv-muted">{drawn ? t("Your card. It stays yours.") : t("{{n}} cards, face down. Tap the deck to draw one.", { n: block.deck ?? 0 })}</p>
    </section>
  );
};
