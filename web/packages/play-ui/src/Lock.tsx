import { useState } from "react";
import { play } from "@zonventure/api";
import type { Translate } from "./feedback.ts";

/**
 * A combination lock: wheels turned up and down through the same symbols,
 * then tried. Only a way of typing — the combination goes to the server
 * as an answer like any other, so a wrong one gets the wrong-answer reply
 * and the lock stays as it was set, to adjust.
 */
export function LockBoard({
  wheels,
  symbols,
  start,
  disabled,
  label,
  t,
  onTry,
}: {
  wheels: number;
  symbols: NonNullable<play.PlayInput["symbols"]>;
  start: number[] | undefined;
  disabled: boolean;
  /** The try button's words while it is disabled for a reason the team should read: "Not there yet". */
  label?: string | undefined;
  t: Translate;
  onTry: (guess: string) => void;
}) {
  const n = symbols.length;
  const [at, setAt] = useState<number[]>(() => Array.from({ length: wheels }, (_, i) => ((start?.[i] ?? 0) % n + n) % n));
  const turn = (w: number, by: number) => setAt((a) => a.map((x, i) => (i === w ? (x + by + n) % n : x)));

  return (
    <div className="pv-lock-wrap">
      <div className="pv-lock" role="group" aria-label={t("Combination lock")}>
        {at.map((s, w) => {
          const sym = symbols[s]!;
          const name = sym.label || sym.value;
          return (
            <div key={w} className="pv-lock__wheel">
              <button type="button" className="pv-lock__turn" disabled={disabled} onClick={() => turn(w, 1)} aria-label={t("Wheel {{n}}: next", { n: w + 1 })}>
                ▲
              </button>
              <div
                className="pv-lock__face"
                tabIndex={disabled ? -1 : 0}
                role="spinbutton"
                aria-label={t("Wheel {{n}}", { n: w + 1 })}
                aria-valuetext={name}
                aria-valuenow={s}
                onKeyDown={(e) => {
                  if (e.key === "ArrowUp") {
                    e.preventDefault();
                    turn(w, 1);
                  } else if (e.key === "ArrowDown") {
                    e.preventDefault();
                    turn(w, -1);
                  }
                }}
              >
                {sym.image ? <img src={play.mediaURL(sym.image.id)} alt={sym.image.alt || name} draggable={false} /> : <span>{name}</span>}
              </div>
              <button type="button" className="pv-lock__turn" disabled={disabled} onClick={() => turn(w, -1)} aria-label={t("Wheel {{n}}: previous", { n: w + 1 })}>
                ▼
              </button>
            </div>
          );
        })}
      </div>
      <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={disabled} onClick={() => onTry(at.map((s) => symbols[s]!.value).join("|"))}>
        {label || t("Try it")}
      </button>
    </div>
  );
}
