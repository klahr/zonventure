import type { Renderer } from "./context.ts";

/** Chosen counters, and — when asked for — what the team carries, from the view. */
export const StatusBlock: Renderer<"status"> = ({ block, ctx }) => {
  const counters = block.counter_values ?? [];
  const items = block.items ? (ctx.items ?? []) : [];
  if (!counters.length && !block.items) return null;
  return (
    <dl className="pv-status">
      {counters.map((c) => (
        <div key={c.slug} className="pv-status__row">
          <dt>{c.name || c.slug}</dt>
          <dd>{Number.isInteger(c.value) ? c.value : c.value.toFixed(1)}</dd>
        </div>
      ))}
      {block.items &&
        (items.length ? (
          items.map((it) => (
            <div key={it.slug} className="pv-status__row">
              <dt>
                {it.icon && <span aria-hidden="true">{it.icon} </span>}
                {it.name || it.slug}
              </dt>
              <dd>{it.qty}</dd>
            </div>
          ))
        ) : (
          <div className="pv-status__row pv-status__row--none">
            <dt>{ctx.t("You are not carrying anything yet.")}</dt>
          </div>
        ))}
    </dl>
  );
};
