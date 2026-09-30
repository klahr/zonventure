import { useEffect, useRef, useState, type TextareaHTMLAttributes } from "react";
import { Markdown } from "@zonventure/play-ui";

/**
 * A textarea for text the phone renders as Markdown. Focused, it is the
 * raw text to edit; otherwise it shows the text as the team will read
 * it, and clicking or tabbing into it goes back to editing. Empty, it
 * stays a textarea so the placeholder shows.
 */
export function MarkdownArea({ value, rows = 2, className, onFocus, onBlur, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement> & { value: string }) {
  const [editing, setEditing] = useState(!!rest.autoFocus);
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!editing || !el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, [editing]);

  if (editing || !value || rest.disabled) {
    return (
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
          onBlur?.(e);
        }}
      />
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
