import { useState } from "react";
import { initials } from "./kit.tsx";
import "./personAvatar.css";

/**
 * A person: their picture in a circle, or their initials on their colour
 * when they have none — or it fails to load. People are circles
 * everywhere, apart from organisations and adventures.
 */
export function PersonAvatar({ name, src, size = 24, color, className }: { name: string; src?: string | undefined; size?: number; color?: string; className?: string }) {
  const [broken, setBroken] = useState<string | null>(null);
  const shown = src && broken !== src ? src : undefined;
  const style = { width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.4)), ...(color ? { "--peer": color } : {}) } as React.CSSProperties;
  return (
    <span className={"person-avatar" + (className ? " " + className : "")} style={style} aria-hidden>
      {shown ? <img src={shown} alt="" onError={() => setBroken(shown)} /> : initials(name.replace(/@.*/, ""))}
    </span>
  );
}
