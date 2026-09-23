// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dazyflow's CommentNode (web/src/components/editor/CommentNode.tsx),
// ported to the designer: a resizable, titled, coloured box behind the
// steps, grouping them on the canvas. It has no ports and the engine never
// reads it. Same markup and classes, so dz.css styles it as in dazyflow.

import { NodeResizer, type Node, type NodeProps } from "@xyflow/react";
import { Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useT } from "../i18n.tsx";
import { Button, ICON } from "./dz/ui.tsx";

export type FrameData = {
  title?: string;
  color?: string;
  onTitleChange?: (title: string) => void;
  onColorChange?: (color: string) => void;
  onRequestDelete?: () => void;
};
export type FrameNode = Node<FrameData, "frame">;

export const FRAME_COLORS = [
  { hex: "#ad1357", name: "Rose" }, // the default; the Zonventure accent
  { hex: "#5a9bd4", name: "Blue" },
  { hex: "#46c46e", name: "Green" },
  { hex: "#e0a45e", name: "Amber" },
  { hex: "#ff6464", name: "Red" },
  { hex: "#9e9abb", name: "Slate" },
] as const;

export const FRAME_COLOR_DEFAULT = FRAME_COLORS[0].hex;

export function FrameCard({ data, selected }: NodeProps<FrameNode>) {
  const t = useT();
  const color = data.color ?? FRAME_COLOR_DEFAULT;
  // Local mirror so typing stays responsive; synced from the document.
  const [title, setTitle] = useState(data.title ?? "");
  useEffect(() => setTitle(data.title ?? ""), [data.title]);

  return (
    <div
      className="dz-frame"
      style={{
        background: `color-mix(in srgb, ${color} 9%, transparent)`,
        borderColor: `color-mix(in srgb, ${color} 55%, var(--dz-border))`,
      }}
    >
      <NodeResizer color={color} isVisible={!!selected} minWidth={160} minHeight={100} />
      <div className="dz-frame-head">
        <input
          className="dz-frame-title nodrag"
          value={title}
          placeholder={t.t("Name this group")}
          spellCheck={false}
          onChange={(e) => {
            setTitle(e.target.value);
            data.onTitleChange?.(e.target.value);
          }}
          style={{ color }}
        />
        {selected && (
          <div className="dz-frame-tools nodrag nopan">
            <div className="dz-frame-colors" role="group" aria-label={t.t("Colour")}>
              {FRAME_COLORS.map((c) => {
                const on = c.hex.toLowerCase() === color.toLowerCase();
                return (
                  <button
                    key={c.hex}
                    type="button"
                    className={"dz-frame-swatch" + (on ? " active" : "")}
                    style={{ background: c.hex }}
                    aria-pressed={on}
                    aria-label={t.t(c.name)}
                    title={t.t(c.name)}
                    onClick={(e) => {
                      e.stopPropagation();
                      data.onColorChange?.(c.hex);
                    }}
                  />
                );
              })}
            </div>
            <Button
              className="dz-frame-delete"
              aria-label={t.t("Delete group")}
              title={t.t("Delete group")}
              onClick={(e) => {
                e.stopPropagation();
                data.onRequestDelete?.();
              }}
            >
              <Trash2 size={ICON.sm} />
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
