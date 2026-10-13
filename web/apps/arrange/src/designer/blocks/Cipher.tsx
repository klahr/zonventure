import type { Cipher } from "@zonventure/graph";
import { Blocks } from "@zonventure/play-ui";
import { withField, type Editor, type Of } from "./types.ts";
import "./cipher-editor.css";

/** What a flasher can send: valid.go's morseable. */
const MORSEABLE = /^[A-Z0-9.,?'!/()&:;=+\-_"$@ \n\t]*$/;

/** The characters of a message a flasher cannot send, once each. */
export function unMorse(text: string): string[] {
  return [...new Set([...text.toUpperCase()].filter((c) => !MORSEABLE.test(c)))];
}

const CIPHERS: { value: Cipher; label: string }[] = [
  { value: "morse", label: "Morse code" },
  { value: "pigpen", label: "Pigpen cipher" },
  { value: "semaphore", label: "Flag semaphore" },
  { value: "caesar", label: "Caesar cipher" },
  { value: "custom", label: "Custom key" },
];

/** Keeps a custom key through a switch, so trying another sheet loses no typing. */
function switchTo(block: Of<"cipher">, cipher: Cipher): Of<"cipher"> {
  const { shift, ...rest } = block;
  return cipher === "caesar" ? { ...rest, cipher, shift: shift ?? 3 } : { ...rest, cipher };
}

export const CipherEditor: Editor<"cipher"> = ({ block, set, t }) => (
  <>
    <select className="field__input" value={block.cipher} aria-label={t.t("Cipher")} onChange={(e) => set(switchTo(block, e.target.value as Cipher))}>
      {CIPHERS.map((c) => (
        <option key={c.value} value={c.value}>
          {t.t(c.label)}
        </option>
      ))}
    </select>
    {block.cipher === "caesar" && (
      <label className="field blk-cipher__shift">
        <span className="field__label">{t.t("Shift")}</span>
        <input
          className="field__input"
          type="number"
          min={1}
          max={25}
          value={block.shift ?? 3}
          onChange={(e) => set({ ...block, shift: Math.min(25, Math.max(1, Math.round(Number(e.target.value)) || 1)) })}
        />
      </label>
    )}
    {block.cipher === "caesar" && (
      <label className="check">
        <input type="checkbox" checked={!!block.wheel} onChange={(e) => set(withField(block, "wheel", e.target.checked))} />
        <span>{t.t("Show as a wheel the team turns")}</span>
      </label>
    )}
    {block.cipher === "caesar" && block.wheel && (
      <p className="field__hint">{t.t("The wheel starts at shift 0: turning it to shift {{n}} is the team's to find.", { n: block.shift ?? 3 })}</p>
    )}
    {block.cipher === "custom" && (
      <textarea
        className="field__input blk-cipher__key"
        rows={6}
        value={block.key ?? ""}
        placeholder={t.t("Title of the key\n★ = A\n☾ = B\n☀ = C")}
        aria-label={t.t("Key")}
        onChange={(e) => set(withField(block, "key", e.target.value))}
      />
    )}
    {block.cipher === "custom" && <p className="field__hint">{t.t("One symbol = meaning per line. A line without = is shown as a heading.")}</p>}
    <div className="blk__preview blk-cipher__preview" aria-label={t.t("Preview")}>
      <Blocks
        blocks={[{ id: "preview", kind: "cipher", cipher: block.cipher, ...(block.shift !== undefined ? { shift: block.shift } : {}), ...(block.key !== undefined ? { key: block.key } : {}), ...(block.cipher === "caesar" && block.wheel ? { wheel: true } : {}) }]}
        t={(k, v) => t.t(k, v)}
        position={undefined}
        placeNote={undefined}
        teamName={undefined}
        now={0}
        items={undefined}
        preview
      />
    </div>
  </>
);

const SPEED = 8;

export const FlasherEditor: Editor<"flasher"> = ({ block, set, t }) => {
  const speed = block.speed ?? SPEED;
  const bad = unMorse(block.text ?? "");
  return (
    <>
      <input className="field__input" value={block.text ?? ""} placeholder={t.t("Such as “SOS AT THE MILL”")} aria-label={t.t("Message")} onChange={(e) => set({ ...block, text: e.target.value })} />
      {bad.length > 0 && <p className="alert alert--danger">{t.t("Morse cannot flash {{chars}}. Use letters, digits and simple punctuation.", { chars: bad.join(" ") })}</p>}
      <label className="field blk__zoom">
        <span className="field__label">{t.t("Speed: {{n}} words a minute", { n: speed })}</span>
        <input type="range" min={1} max={30} value={speed} onChange={(e) => set(withField(block, "speed", Number(e.target.value) === SPEED ? undefined : Number(e.target.value)))} />
        <span className="blk__zoom-ends" aria-hidden="true">
          <span>{t.t("Slow")}</span>
          <span>{t.t("Fast")}</span>
        </span>
      </label>
      <p className="field__hint">{t.t("The screen flashes the message in Morse, and Android phones vibrate it too. Add a Morse cipher sheet if the team needs the key.")}</p>
    </>
  );
};

export const LettersEditor: Editor<"letters"> = ({ block, set, t }) => (
  <>
    <input className="field__input" value={block.text ?? ""} placeholder={t.t("Such as “SILENT”")} aria-label={t.t("Letters")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <p className="field__hint">{t.t("Letters are shown as tiles in scrambled order.")}</p>
  </>
);
