import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type TextareaHTMLAttributes } from "react";
import { Bold, Code, Heading2, Italic, Link2, List, ListOrdered } from "lucide-react";
import { createPortal } from "react-dom";
import { Markdown } from "@zonventure/play-ui";
import { useT } from "../i18n.tsx";
import { format, type Edit, type Format } from "../mdFormat.ts";
import "./mdtoolbar.css";

/**
 * A textarea for text the phone renders as Markdown. Focused, it is the
 * raw text to edit; otherwise it shows the text as the team will read
 * it, and clicking or tabbing into it goes back to editing. Empty, it
 * stays a textarea so the placeholder shows.
 *
 * Selecting text brings up a small menu over it — bold, italic, code,
 * link, heading, lists — that writes the Markdown for the author, and
 * Ctrl/Cmd+B, I and K do the same from the keyboard.
 */
export function MarkdownArea({ value, rows = 2, className, onFocus, onBlur, onKeyDown, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement> & { value: string }) {
  const [editing, setEditing] = useState(!!rest.autoFocus);
  const ref = useRef<HTMLTextAreaElement>(null);
  const [menu, setMenu] = useState<{ x: number; y: number; below: boolean } | null>(null);
  const dragging = useRef(false);
  useEffect(() => {
    const el = ref.current;
    if (!editing || !el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, [editing]);

  // Where the menu goes: over the selection's first line, centred on it
  // when it is on one line — below it when there is no room above.
  const place = useCallback(() => {
    const el = ref.current;
    if (!el || dragging.current || el.selectionStart === el.selectionEnd || document.activeElement !== el) return setMenu(null);
    // In the window's coordinates: the menu floats over everything, so
    // a narrow inspector or a scrolling panel never cuts it off.
    const box = el.getBoundingClientRect();
    const a = caretAt(el, el.selectionStart);
    const b = caretAt(el, el.selectionEnd);
    const x = box.left + (a.top === b.top ? (a.left + b.left) / 2 : a.left);
    const top = box.top + Math.max(0, a.top);
    const below = top < MENU_HEIGHT + 12;
    const half = MENU_WIDTH / 2 + 8;
    setMenu({
      x: Math.max(half, Math.min(x, window.innerWidth - half)),
      y: below ? box.top + Math.min(b.top + b.height, el.clientHeight) + 6 : top - 6,
      below,
    });
  }, []);

  // A selection made by dragging settles on mouse up, wherever that is.
  useEffect(() => {
    if (!editing) return;
    const up = () => {
      if (!dragging.current) return;
      dragging.current = false;
      requestAnimationFrame(place);
    };
    // Any other change of selection — the keyboard, a double click, a
    // script — follows too, once the browser says so.
    const changed = () => {
      if (document.activeElement === ref.current) place();
    };
    // Scrolling anything moves the text out from under the menu.
    const away = () => setMenu(null);
    window.addEventListener("mouseup", up);
    document.addEventListener("selectionchange", changed);
    window.addEventListener("scroll", away, true);
    window.addEventListener("resize", away);
    return () => {
      window.removeEventListener("mouseup", up);
      document.removeEventListener("selectionchange", changed);
      window.removeEventListener("scroll", away, true);
      window.removeEventListener("resize", away);
    };
  }, [editing, place]);

  const run = (f: Format) => {
    const el = ref.current;
    if (!el) return;
    applyEdit(el, format(el.value, el.selectionStart, el.selectionEnd, f));
    requestAnimationFrame(place);
  };

  const keys = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    onKeyDown?.(e);
    if (e.defaultPrevented) return;
    const mod = e.metaKey || e.ctrlKey;
    const f = mod && !e.altKey && !e.shiftKey ? ({ b: "bold", i: "italic", k: "link" } as Record<string, Format>)[e.key.toLowerCase()] : undefined;
    if (f) {
      e.preventDefault();
      // Not the designer's own Ctrl+K: here it makes a link.
      e.stopPropagation();
      run(f);
      return;
    }
    if (e.key === "Escape" && menu) {
      e.stopPropagation();
      setMenu(null);
    }
  };

  if (editing || !value || rest.disabled) {
    return (
      <span className="md-wrap">
        <textarea
          ref={ref}
          {...rest}
          className={className}
          rows={rows}
          value={value}
          onFocus={(e) => {
            setEditing(true);
            onFocus?.(e);
          }}
          onBlur={(e) => {
            setEditing(false);
            setMenu(null);
            onBlur?.(e);
          }}
          onMouseDown={() => {
            dragging.current = true;
            setMenu(null);
          }}
          onKeyUp={place}
          onSelect={place}
          onScroll={() => setMenu(null)}
          onKeyDown={keys}
        />
        {menu && !rest.readOnly && createPortal(<FormatMenu x={menu.x} y={menu.y} below={menu.below} onFormat={run} />, document.body)}
      </span>
    );
  }
  return (
    <div
      className={(className ? className + " " : "") + "md-area"}
      style={{ minHeight: `${rows * 1.4 + 1.5}em` }}
      tabIndex={0}
      role="textbox"
      aria-label={rest["aria-label"]}
      aria-multiline="true"
      onFocus={() => setEditing(true)}
      onClick={(e) => e.preventDefault()}
    >
      <Markdown text={value} />
    </div>
  );
}

/** The menu's size, for keeping it on screen before it is drawn. */
const MENU_WIDTH = 236;
const MENU_HEIGHT = 34;

const mac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
const MOD = mac ? "⌘" : "Ctrl+";

/** The menu over a selection: each button formats it. Pressing one keeps the text box focused. */
function FormatMenu({ x, y, below, onFormat }: { x: number; y: number; below: boolean; onFormat: (f: Format) => void }) {
  const t = useT();
  const items: { f: Format; icon: typeof Bold; label: string; key?: string }[] = [
    { f: "bold", icon: Bold, label: t.t("Bold"), key: "B" },
    { f: "italic", icon: Italic, label: t.t("Italic"), key: "I" },
    { f: "code", icon: Code, label: t.t("Code") },
    { f: "link", icon: Link2, label: t.t("Link"), key: "K" },
    { f: "heading", icon: Heading2, label: t.t("Heading") },
    { f: "bullets", icon: List, label: t.t("Bulleted list") },
    { f: "numbers", icon: ListOrdered, label: t.t("Numbered list") },
  ];
  return (
    <span
      className={"md-menu" + (below ? " is-below" : "")}
      style={{ left: x, top: y }}
      role="toolbar"
      aria-label={t.t("Formatting")}
      // Keep the text box focused, and its selection with it.
      onMouseDown={(e) => e.preventDefault()}
    >
      {items.map((it, i) => (
        <button
          key={it.f}
          type="button"
          className={"md-menu__btn" + (i === 4 ? " md-menu__btn--split" : "")}
          onClick={() => onFormat(it.f)}
          aria-label={it.label}
          title={it.key ? `${it.label} (${MOD}${it.key})` : it.label}
          tabIndex={-1}
        >
          <it.icon size={15} strokeWidth={2.4} aria-hidden />
        </button>
      ))}
    </span>
  );
}

/**
 * Make an edit in the textarea as typing would: through the browser's
 * own insert, so Ctrl+Z undoes it, and so React hears the change.
 */
function applyEdit(el: HTMLTextAreaElement, ed: Edit) {
  el.focus();
  el.setSelectionRange(ed.from, ed.to);
  const typed = typeof document.execCommand === "function" && document.execCommand("insertText", false, ed.insert);
  if (!typed || el.value.slice(ed.from, ed.from + ed.insert.length) !== ed.insert) {
    el.setRangeText(ed.insert, ed.from, ed.to, "end");
    el.dispatchEvent(new Event("input", { bubbles: true }));
  }
  el.setSelectionRange(ed.selStart, ed.selEnd);
}

const COPY = [
  "boxSizing", "width", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth",
  "fontFamily", "fontSize", "fontWeight", "fontStyle", "lineHeight", "letterSpacing", "textTransform", "wordSpacing", "tabSize", "textIndent",
] as const;

/**
 * Where character i of a textarea sits, relative to the textarea's box:
 * measured on an invisible copy of it, laid out the same way, because a
 * textarea does not say where its text is.
 */
function caretAt(el: HTMLTextAreaElement, i: number): { left: number; top: number; height: number } {
  const cs = getComputedStyle(el);
  const div = document.createElement("div");
  const st = div.style;
  for (const k of COPY) st[k] = cs[k];
  st.position = "absolute";
  st.visibility = "hidden";
  st.whiteSpace = "pre-wrap";
  st.overflowWrap = "break-word";
  st.top = "0";
  st.left = "-9999px";
  div.textContent = el.value.slice(0, i);
  const mark = document.createElement("span");
  mark.textContent = el.value.slice(i, i + 1) || "";
  div.appendChild(mark);
  document.body.appendChild(div);
  const out = { left: mark.offsetLeft - el.scrollLeft, top: mark.offsetTop - el.scrollTop, height: mark.offsetHeight };
  div.remove();
  return out;
}
