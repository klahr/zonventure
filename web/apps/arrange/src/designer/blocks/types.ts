import type { ReactNode } from "react";
import type { Block, BlockKind, Graph, Node, Zone } from "@zonventure/graph";
import type { I18n } from "@zonventure/i18n";
import type { LucideIcon } from "lucide-react";

export type T = I18n;
export type Of<K extends BlockKind> = Block & { kind: K };

export interface EditorProps<K extends BlockKind> {
  block: Of<K>;
  set: (b: Of<K>) => void;
  t: T;
  graph: Graph;
  /** The step the block is on, as it is now. */
  node: Node;
  /** What the step calls its text: "Text", "Closing text". */
  textLabel: string;
  /**
   * Writes the adventure's zones and this block in one edit, for a block
   * that makes or moves its own place. Absent where the editor can't.
   */
  withZones?: ((zones: Zone[], b: Of<K>) => void) | undefined;
}

/** A block's fields, edited, and a preview where one helps. */
export type Editor<K extends BlockKind> = (p: EditorProps<K>) => ReactNode;

/** What the add menu groups kinds under, in order. */
export type Group = "Content" | "Quiz" | "Place" | "Props" | "Gadgets" | "Game";

export interface KindDef<K extends BlockKind> {
  label: string;
  icon: LucideIcon;
  group: Group;
  /** One line in the add menu. */
  about: string;
  editor: Editor<K>;
}

/** Sets a field, or removes it when the value is its default. */
export function withField<B extends object, K extends keyof B>(b: B, key: K, value: B[K] | undefined): B {
  const { [key]: _, ...rest } = b;
  return (value === undefined || value === "" || value === false ? rest : { ...rest, [key]: value }) as B;
}
