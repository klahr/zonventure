import { useRef } from "react";
import { useT } from "../i18n.tsx";
import { Button } from "../designer/dz/ui.tsx";
import "./imageDrop.css";

/**
 * Where a picture is set: a dashed box that is itself the button, showing
 * the picture once there is one, with Remove under it. A logo fits inside
 * its square, a photo fills it, and a cover is 16:9.
 */
export function ImageDrop(props: {
  src?: string | undefined;
  shape: "logo" | "photo" | "cover";
  label: string;
  title?: string;
  accept?: string;
  busy?: boolean;
  disabled?: boolean;
  onFile: (f: File) => void;
  onRemove?: (() => void) | undefined;
  removeLabel?: string;
}) {
  const t = useT();
  const input = useRef<HTMLInputElement>(null);
  const { src, shape } = props;
  return (
    <div className={`img-drop img-drop--${shape}`}>
      <button
        type="button"
        className={`img-drop__box img-drop__box--${shape}` + (src ? " has-image" : "") + (props.busy ? " is-busy" : "")}
        style={src && shape === "cover" ? { backgroundImage: `url('${src}')` } : undefined}
        aria-label={props.label}
        title={props.title ?? props.label}
        disabled={props.disabled || props.busy}
        onClick={() => input.current?.click()}
      >
        {src && shape !== "cover" && <img src={src} alt="" />}
        {!src && <span>{props.busy ? t.t("Uploading…") : t.t("Click to upload")}</span>}
      </button>
      <input
        ref={input}
        type="file"
        hidden
        accept={props.accept ?? "image/png,image/jpeg,image/webp"}
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (f) props.onFile(f);
        }}
      />
      {src && props.onRemove && !props.disabled && (
        <Button size="sm" variant="ghost" disabled={props.busy} onClick={props.onRemove}>{props.removeLabel ?? t.t("Remove")}</Button>
      )}
    </div>
  );
}
