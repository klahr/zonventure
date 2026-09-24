import type { Block, Graph, Predicate } from "@zonventure/graph";
import { ConditionEditor } from "../ConditionEditor.tsx";
import type { T } from "./types.ts";

/** Waiting a minute is the commonest rule, and a clear example of one. */
const FIRST: Predicate = { op: "node_elapsed", cmp: "gte", n: 60 };

/**
 * When a block appears. The server holds a hidden block back — text and
 * file — so this is safe for a clue that must wait: near a zone, after a
 * while on the step, once the team carries something.
 */
export function ShowEditor({ block, set, graph, t }: { block: Block; set: (b: Block) => void; graph: Graph; t: T }) {
  const show = block.show;
  return (
    <div className="blk__show">
      <label className="check">
        <input
          type="checkbox"
          checked={!!show}
          onChange={(e) => {
            const { show: _, ...rest } = block;
            set((e.target.checked ? { ...rest, show: { when: FIRST } } : rest) as Block);
          }}
        />
        <span>{t.t("Show only when…")}</span>
      </label>
      {show && (
        <>
          <ConditionEditor graph={graph} value={show.when} onChange={(when) => set({ ...block, show: { ...show, when } })} />
          <label className="check">
            <input
              type="checkbox"
              checked={!!show.tease}
              onChange={(e) => {
                const { tease: _, ...rest } = show;
                set({ ...block, show: e.target.checked ? { ...rest, tease: true } : rest });
              }}
            />
            <span>{t.t("Show that something is hidden here until then")}</span>
          </label>
        </>
      )}
    </div>
  );
}
