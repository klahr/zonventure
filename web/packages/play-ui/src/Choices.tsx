import { useState } from "react";
import type { play } from "@zonventure/api";
import type { Translate } from "./feedback.ts";

/** Past this many options a row of buttons stops fitting; use a list. */
const BUTTONS_UP_TO = 6;

/**
 * Questions with fixed options, instead of a text box. One question is
 * answered by tapping an option; several are picked first and sent
 * together, as one guess — the values joined by "|", which the server
 * marks. The phone never knows which options are right.
 */
export function ChoiceForm({
  fields,
  disabled,
  t,
  onAnswer,
}: {
  fields: NonNullable<play.PlayInput["fields"]>;
  disabled: boolean;
  t: Translate;
  onAnswer: (guess: string) => void;
}) {
  const [picked, setPicked] = useState<string[]>(() => fields.map(() => ""));
  const single = fields.length === 1;
  const pick = (i: number, v: string) => {
    if (single) {
      onAnswer(v);
      return;
    }
    setPicked((p) => p.map((x, j) => (j === i ? v : x)));
  };

  return (
    <div className="pv-choices">
      {fields.map((f, i) => (
        <fieldset key={i} className="pv-choice" disabled={disabled}>
          {f.label && <legend className="pv-choice__label">{f.label}</legend>}
          {f.options.length <= BUTTONS_UP_TO ? (
            <div className="pv-choice__options">
              {f.options.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  className={"pv-choice__opt" + (picked[i] === o.value ? " is-on" : "")}
                  aria-pressed={single ? undefined : picked[i] === o.value}
                  onClick={() => pick(i, o.value)}
                >
                  {o.label || o.value}
                </button>
              ))}
            </div>
          ) : (
            <select className="pv-input" value={picked[i]} aria-label={f.label || t("Pick one")} onChange={(e) => (single ? e.target.value && onAnswer(e.target.value) : pick(i, e.target.value))}>
              <option value="">{t("Pick one…")}</option>
              {f.options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label || o.value}
                </option>
              ))}
            </select>
          )}
        </fieldset>
      ))}
      {!single && (
        <button type="button" className="pv-btn pv-btn--primary pv-btn--wide" disabled={disabled || picked.some((p) => !p)} onClick={() => onAnswer(picked.join("|"))}>
          {t("Send in")}
        </button>
      )}
    </div>
  );
}
