import type { ReactNode } from "react";
import { ALPHABET, MORSE, SEMAPHORE, armVector, caesarAlphabet, caesarShift, parseKey, pigpen } from "../ciphers.ts";
import type { Translate } from "../feedback.ts";
import type { Renderer } from "./context.ts";
import "./cipher.css";

/** A cipher's key sheet, to decode with; the puzzle is elsewhere. */
export const CipherBlock: Renderer<"cipher"> = ({ block, ctx }) => {
  const { t } = ctx;
  switch (block.cipher) {
    case "morse":
      return <Sheet kind="morse" title={t("Morse code")}><Morse /></Sheet>;
    case "pigpen":
      return <Sheet kind="pigpen" title={t("Pigpen cipher")}><Glyphs draw={PigpenGlyph} /></Sheet>;
    case "semaphore":
      return <Sheet kind="semaphore" title={t("Flag semaphore")}><Glyphs draw={SemaphoreGlyph} /></Sheet>;
    case "caesar": {
      const shift = caesarShift(block.shift);
      return <Sheet kind="caesar" title={t("Caesar cipher, shift {{n}}", { n: shift })}><Caesar shift={shift} t={t} /></Sheet>;
    }
    case "custom":
      return <Custom text={block.key} />;
    default:
      return null;
  }
};

function Sheet({ kind, title, children }: { kind: string; title: string; children: ReactNode }) {
  return (
    <section className={`pv-cipher pv-cipher--${kind}`} aria-label={title}>
      <p className="pv-cipher__title">{title}</p>
      {children}
    </section>
  );
}

function Morse() {
  return (
    <dl className="pv-cipher__grid">
      {[...ALPHABET, ..."0123456789"].map((c) => (
        <div key={c} className="pv-cipher__morse">
          <dt>{c}</dt>
          <dd>{MORSE[c]}</dd>
        </div>
      ))}
    </dl>
  );
}

function Glyphs({ draw: Draw }: { draw: (p: { letter: string }) => ReactNode }) {
  return (
    <ul className="pv-cipher__grid">
      {[...ALPHABET].map((c) => (
        <li key={c} className="pv-cipher__cell">
          <Draw letter={c} />
          <span>{c}</span>
        </li>
      ))}
    </ul>
  );
}

/** Drawn inset in a 24-unit box so a stroke on the edge is not clipped. */
function PigpenGlyph({ letter }: { letter: string }) {
  const g = pigpen(letter);
  if (!g) return null;
  const at = (n: number) => 4 + n * 16;
  return (
    <svg className="pv-cipher__glyph" viewBox="0 0 24 24" aria-hidden="true">
      {g.segments.map(([x1, y1, x2, y2], i) => (
        <line key={i} x1={at(x1)} y1={at(y1)} x2={at(x2)} y2={at(y2)} />
      ))}
      {g.dot && <circle cx={at(g.dot[0])} cy={at(g.dot[1])} r={1.8} />}
    </svg>
  );
}

const SHOULDER: [number, number] = [20, 17];
const ARM = 13;
const FLAG = 6;

/** A stick figure holding two flags, each split red over yellow as at sea. */
function SemaphoreGlyph({ letter }: { letter: string }) {
  const arms = SEMAPHORE[letter];
  if (!arms) return null;
  const [sx, sy] = SHOULDER;
  return (
    <svg className="pv-cipher__glyph pv-cipher__figure" viewBox="0 0 40 46" aria-hidden="true">
      <circle cx={20} cy={9} r={4} />
      <line x1={20} y1={14} x2={20} y2={33} />
      <line x1={20} y1={33} x2={15} y2={44} />
      <line x1={20} y1={33} x2={25} y2={44} />
      {arms.map((p) => {
        const [dx, dy] = armVector(p);
        const ex = sx + dx * ARM;
        const ey = sy + dy * ARM;
        // The flag hangs off one side of the pole's end.
        const bx = ex - dx * FLAG;
        const by = ey - dy * FLAG;
        const px = -dy * FLAG;
        const py = dx * FLAG;
        return (
          <g key={p}>
            <line x1={sx} y1={sy} x2={ex} y2={ey} />
            <polygon className="pv-cipher__flag-a" points={`${ex},${ey} ${bx},${by} ${bx + px},${by + py}`} />
            <polygon className="pv-cipher__flag-b" points={`${ex},${ey} ${ex + px},${ey + py} ${bx + px},${by + py}`} />
          </g>
        );
      })}
    </svg>
  );
}

/** Two bands of thirteen, so the columns stay wide enough on a phone. */
function Caesar({ shift, t }: { shift: number; t: Translate }) {
  const under = caesarAlphabet(shift);
  return (
    <div className="pv-cipher__caesar">
      {[0, 13].map((from) => (
        <div key={from} className="pv-cipher__band">
          <span className="pv-cipher__row-label">{t("Plain")}</span>
          {[...ALPHABET.slice(from, from + 13)].map((c) => (
            <span key={c}>{c}</span>
          ))}
          <span className="pv-cipher__row-label">{t("Cipher")}</span>
          {[...under.slice(from, from + 13)].map((c, i) => (
            <span key={i} className="pv-cipher__shifted">
              {c}
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

function Custom({ text }: { text: string | undefined }) {
  const lines = parseKey(text);
  if (!lines.length) return null;
  return (
    <section className="pv-cipher pv-cipher--custom">
      <div className="pv-cipher__key">
        {lines.map((l, i) =>
          "note" in l ? (
            <p key={i} className="pv-cipher__note">
              {l.note}
            </p>
          ) : (
            <p key={i} className="pv-cipher__pair">
              <span className="pv-cipher__symbol">{l.symbol}</span>
              <span>{l.meaning}</span>
            </p>
          ),
        )}
      </div>
    </section>
  );
}
