import type { Cipher } from "@zonventure/graph";
import { Blocks } from "@zonventure/play-ui";
import { withField, type Editor, type Of } from "./types.ts";
import "./cipher-editor.css";

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
        blocks={[{ id: "preview", kind: "cipher", cipher: block.cipher, ...(block.shift !== undefined ? { shift: block.shift } : {}), ...(block.key !== undefined ? { key: block.key } : {}) }]}
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
