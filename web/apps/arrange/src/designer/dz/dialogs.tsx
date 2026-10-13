// SPDX-FileCopyrightText: 2026 Angels' Ware
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dazyflow's release dialogs — PublishLabelModal, DiffDialog and
// PublishCelebration (web/src/components/editor, web/src/pages/flows/
// editor) — plus its settings and help modals, ported to the adventure
// designer. Same markup and classes, so ../dz.css styles them as in
// dazyflow; wording adapted from flows to adventures.

import { useEffect, useRef, useState, type ReactNode } from "react";
import { GitCompare, Rocket, X } from "lucide-react";
import { diffIsEmpty, wordDiff, type GraphDiff, type TextChange } from "@zonventure/graph";
import { useT } from "../../i18n.tsx";
import { Button, Callout, DzPortal, ICON, Notice, useEscapeToClose } from "./ui.tsx";

export function PublishLabelModal({
  title,
  message,
  confirmLabel,
  warning,
  blocked = false,
  onPublish,
  onCancel,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  warning?: ReactNode;
  blocked?: boolean;
  onPublish: (label: string) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  useEscapeToClose(onCancel);
  return (
    <DzPortal>
      <div className="modal-backdrop" onClick={onCancel}>
        <div className="modal confirm-dialog" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
          <div className="modal-head">
            <h2>{title}</h2>
          </div>
          <div className="modal-body">
            {warning && <Callout variant="warning">{warning}</Callout>}
            <label className="publish-label-field">
              <span>{t.t("Name this release (optional)")}</span>
              <input
                ref={input}
                type="text"
                value={value}
                placeholder={t.t("e.g. Summer edition")}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !blocked) onPublish(value.trim());
                }}
              />
            </label>
            <p className="confirm-message">{message}</p>
          </div>
          <div className="modal-foot">
            <Button onClick={onCancel}>{t.t("Cancel")}</Button>
            <Button variant="primary" disabled={blocked} onClick={() => onPublish(value.trim())}>
              <Rocket size={ICON.sm} />
              {confirmLabel}
            </Button>
          </div>
        </div>
      </div>
    </DzPortal>
  );
}

export function DiffDialog({
  diff,
  loading,
  publishing,
  canPublish,
  onPublish,
  onClose,
}: {
  diff: GraphDiff | null;
  loading: boolean;
  publishing: boolean;
  canPublish: boolean;
  onPublish: () => void;
  onClose: () => void;
}) {
  const t = useT();
  useEscapeToClose(onClose);
  const kinds: Record<string, string> = { zones: t.t("Zones"), items: t.t("Items"), counters: t.t("Shared counters"), journal: t.t("Journal"), time_limit: t.t("Time limit"), hide_score: t.t("Score on the phone") };
  return (
    <DzPortal>
      <div className="modal-backdrop" onClick={onClose}>
        <div className="modal" onClick={(e) => e.stopPropagation()} aria-modal="true" role="dialog" aria-label={t.t("Changes")}>
          <div className="modal-head">
            <strong>
              <GitCompare className="icon-lede" size={ICON.sm} />
              {t.t("Draft changes since published")}
            </strong>
            <Button size="icon" onClick={onClose} aria-label={t.t("Dismiss")}>
              <X size={ICON.md} />
            </Button>
          </div>
          <div className="modal-body">
            {loading ? (
              <Notice>{t.t("Loading…")}</Notice>
            ) : !diff || diffIsEmpty(diff) ? (
              <Notice>{t.t("The draft matches the published version.")}</Notice>
            ) : (
              <ul className="diff-list">
                {diff.added.map((s) => (
                  <li key={`a-${s.slug}`} className="diff-row added">
                    + {t.t("Step added: {{name}}", { name: s.title })}
                  </li>
                ))}
                {diff.removed.map((s) => (
                  <li key={`r-${s.slug}`} className="diff-row removed">
                    − {t.t("Step removed: {{name}}", { name: s.title })}
                  </li>
                ))}
                {diff.changed.map((s) => (
                  <li key={`c-${s.slug}`} className="diff-row changed">
                    ~ {t.t("Step changed: {{name}} ({{what}})", { name: s.title, what: s.fields.map((f) => t.t(f)).join(", ") })}
                    {s.texts.length > 0 && <TextChanges changes={s.texts} before={t.t("Published")} after={t.t("Draft")} />}
                  </li>
                ))}
                {diff.addedEdges.map((k) => (
                  <li key={`ae-${k}`} className="diff-row added">
                    + {t.t("Connection added")}: <code>{k}</code>
                  </li>
                ))}
                {diff.removedEdges.map((k) => (
                  <li key={`re-${k}`} className="diff-row removed">
                    − {t.t("Connection removed")}: <code>{k}</code>
                  </li>
                ))}
                {diff.resources.length > 0 && (
                  <li className="diff-row changed">~ {t.t("Adventure settings changed: {{what}}", { what: diff.resources.map((r) => kinds[r] ?? r).join(", ") })}</li>
                )}
              </ul>
            )}
          </div>
          <div className="modal-foot">
            <Button variant="ghost" onClick={onClose}>
              {t.t("Dismiss")}
            </Button>
            <Button
              className="editor-publish"
              onClick={() => {
                onClose();
                onPublish();
              }}
              disabled={publishing || !canPublish}
            >
              <Rocket size={ICON.sm} />
              {t.t("Publish")}
            </Button>
          </div>
        </div>
      </div>
    </DzPortal>
  );
}

/**
 * A step's texts in two versions, field by field: the old with the words
 * that went struck through, the new with the words that came marked.
 */
export function TextChanges({ changes, before, after }: { changes: TextChange[]; before: string; after: string }) {
  const t = useT();
  return (
    <dl className="text-diff">
      {changes.map((c) => {
        const parts = wordDiff(c.before, c.after);
        const side = (label: string, text: string, Mark: "del" | "ins") => (
          <dd className={`text-diff__${Mark}`}>
            <span className="text-diff__side">{label}</span>
            {text ? parts.map((p, i) => (p.op === "same" ? <span key={i}>{p.text}</span> : p.op === Mark ? <Mark key={i}>{p.text}</Mark> : null)) : <em>{t.t("(empty)")}</em>}
          </dd>
        );
        return (
          <div key={c.key} className="text-diff__field">
            <dt>{t.t(c.label, c.vars)}</dt>
            {side(before, c.before, "del")}
            {side(after, c.after, "ins")}
          </div>
        );
      })}
    </dl>
  );
}

/** The one-shot card when a publish lands (dazyflow PublishCelebration). */
export function PublishCelebration() {
  const t = useT();
  return (
    <DzPortal>
      <div className="publish-live" aria-hidden="true">
        <div className="publish-live-card">
          <span className="publish-live-mark">
            <span className="publish-live-halo" />
            <svg viewBox="0 0 32 32" className="publish-live-check" aria-hidden="true">
              <circle className="publish-live-check-ring" cx="16" cy="16" r="14.5" fill="none" strokeWidth="1.5" />
              <path className="publish-live-check-tick" d="M10 16.6l4.2 4.2L22.4 12" fill="none" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </span>
          <span className="publish-live-words">
            <strong className="publish-live-title">{t.t("Published")}</strong>
            <span className="publish-live-sub">{t.t("Events that start from now on play this version.")}</span>
          </span>
        </div>
      </div>
    </DzPortal>
  );
}

/** A plain modal with a title and a close button, for settings and help. */
export function PanelModal({ title, wide, onClose, children }: { title: string; wide?: boolean; onClose: () => void; children: ReactNode }) {
  const t = useT();
  useEscapeToClose(onClose);
  return (
    <DzPortal>
      <div className="modal-backdrop" onClick={onClose}>
        <div className={"modal" + (wide ? " modal-wide" : "")} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={title}>
          <div className="modal-head">
            <strong>{title}</strong>
            <Button size="icon" onClick={onClose} aria-label={t.t("Close")}>
              <X size={ICON.md} />
            </Button>
          </div>
          <div className="modal-body">{children}</div>
        </div>
      </div>
    </DzPortal>
  );
}

/** Keyboard shortcuts — dazyflow's HelpModal list, for the designer. */
export function HelpModal({ onClose }: { onClose: () => void }) {
  const t = useT();
  const rows: [string, string][] = [
    ["Ctrl/Cmd+K", t.t("Add a step")],
    ["/", t.t("Find a step and open it")],
    ["A", t.t("Add the next step, one field at a time")],
    [t.t("Right-click the canvas"), t.t("Add a step at that spot")],
    [t.t("Drag from an output"), t.t("Add what comes next, already connected")],
    ["Ctrl/Cmd+Z", t.t("Undo")],
    ["Ctrl/Cmd+Shift+Z", t.t("Redo")],
    ["Ctrl/Cmd+C / V", t.t("Copy and paste steps")],
    ["Ctrl/Cmd+D", t.t("Duplicate the selected step")],
    ["Del", t.t("Delete the selection")],
    ["H J K L / " + t.t("Arrow keys"), t.t("Go to the previous step, the branch below, the branch above, the next step")],
    [t.t("Shift+arrows"), t.t("Move the selected step")],
    ["?", t.t("Show this list")],
  ];
  return (
    <PanelModal title={t.t("Keyboard shortcuts")} onClose={onClose}>
      <dl className="dz-shortcuts">
        {rows.map(([k, v]) => (
          <div key={k} className="dz-shortcut">
            <dt>
              <kbd>{k}</kbd>
            </dt>
            <dd>{v}</dd>
          </div>
        ))}
      </dl>
    </PanelModal>
  );
}
