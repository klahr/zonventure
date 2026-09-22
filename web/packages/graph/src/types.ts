// The adventure graph, as the server sends and accepts it.
//
// Mirrors internal/graph (graph.go), internal/graph/match and
// internal/graph/cond field for field, including the JSON names. Keep it
// that way: graph.ParseEnvelope rejects unknown fields, so a property
// added here without a Go counterpart makes every save fail, and one
// renamed here silently drops data on the next save.
//
// The shapes are hand-written rather than generated. With the domain
// settled that trade is now close; if these start drifting, generating
// them from the Go structs (tygo) is the fix, not more care.

export type NodeType = "start" | "puzzle" | "story" | "hub" | "gate" | "effect" | "end";

export const PORT_OUT = "out";
export const PORT_PASS = "pass";
export const PORT_FAIL = "fail";
export const PORT_DONE = "done";

export interface Graph {
  nodes: Node[];
  edges: Edge[];
  zones?: Zone[];
  items?: Item[];
  counters?: Counter[];
  journal?: JournalEntry[];
}

export interface Envelope {
  format: "zonventure-adventure-v2";
  graph: Graph;
}

export const FORMAT_V2 = "zonventure-adventure-v2" as const;

export interface Node {
  slug: string;
  type: NodeType;
  title?: string;
  x?: number;
  y?: number;
  puzzle?: PuzzleConfig;
  story?: StoryConfig;
  hub?: HubConfig;
  gate?: GateConfig;
  effect?: EffectConfig;
  end?: EndConfig;
}

export interface Edge {
  from: string;
  from_port: string;
  to: string;
}

export type Trigger = "answer" | "timeout" | "skipped" | "attempts";

export interface Output {
  port: string;
  label?: string;
  trigger: Trigger;
  matchers?: Matcher[];
  zone?: string;
  inverted?: boolean;
  after_attempts?: number;
  points?: number;
}

export interface Response {
  after_attempts?: number;
  text: string;
}

export interface Hint {
  text: string;
  delay_seconds?: number;
  cost?: number;
}

export interface PuzzleConfig {
  prompt?: string;
  zone?: string;
  outputs?: Output[];
  responses?: Response[];
  hints?: Hint[];
  time_limit_seconds?: number;
  skip_after_guesses?: number;
  skip_after_seconds?: number;
  lockout_seconds?: number;
  repeatable?: boolean;
}

export interface StoryConfig {
  text?: string;
  zone?: string;
}

export type Policy = "choice" | "sequence" | "shuffle" | "spread" | "capacity" | "random";

export interface Branch {
  port: string;
  label?: string;
  condition?: Predicate;
  capacity?: number;
}

export interface HubConfig {
  policy: Policy;
  branches?: Branch[];
  required?: number;
  hide_locked?: boolean;
}

export interface GateConfig {
  condition: Predicate;
}

export type EffectKind =
  | "grant_item"
  | "score"
  | "counter"
  | "journal"
  | "email"
  | "theme"
  | "webhook";

export interface EffectConfig {
  kind: EffectKind;
  item?: string;
  qty?: number;
  points?: number;
  counter?: string;
  op?: "set" | "add";
  value?: number;
  journal_entry?: string;
  template?: string;
  url?: string;
  theme?: string;
}

export interface EndConfig {
  outcome?: "complete" | "fail";
  text?: string;
}

export interface Zone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
}

export interface Item {
  slug: string;
  name?: string;
  icon?: string;
  description?: string;
}

export interface Counter {
  slug: string;
  name?: string;
  initial?: number;
  min?: number;
  max?: number;
}

export interface JournalEntry {
  slug: string;
  title?: string;
  body?: string;
}

export type MatcherKind =
  | "exact"
  | "contains"
  | "regex"
  | "interval"
  | "near"
  | "set"
  | "anything";

export interface Matcher {
  kind: MatcherKind;
  value?: string;
  low?: number;
  high?: number;
  target?: number;
  tolerance?: number;
  members?: string[];
  required?: number;
  ordered?: boolean;
}

export type Op =
  | ""
  | "all"
  | "any"
  | "not"
  | "solved"
  | "visited"
  | "outcome"
  | "attempts"
  | "item"
  | "counter"
  | "score"
  | "elapsed"
  | "node_elapsed"
  | "team_size"
  | "in_zone"
  | "time_of_day"
  | "weekday";

export type Cmp = "eq" | "ne" | "lt" | "lte" | "gt" | "gte";

export interface Predicate {
  op: Op;
  of?: Predicate[];
  node?: string;
  item?: string;
  counter?: string;
  zone?: string;
  cmp?: Cmp;
  n?: number;
  is?: string;
  members?: string[];
}

// internal/graph/valid

export interface Issue {
  severity: "error" | "warning";
  code: string;
  node?: string;
  port?: string;
  message: string;
}

export interface Report {
  issues: Issue[] | null;
}
