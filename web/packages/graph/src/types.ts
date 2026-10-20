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

export type NodeType = "start" | "puzzle" | "story" | "hub" | "gate" | "effect" | "switch" | "end";

export const PORT_OUT = "out";
export const PORT_PASS = "pass";
export const PORT_FAIL = "fail";
export const PORT_DONE = "done";
export const PORT_ELSE = "else";

export interface Graph {
  nodes: Node[];
  edges: Edge[];
  zones?: Zone[];
  items?: Item[];
  counters?: Counter[];
  journal?: JournalEntry[];
  /** The parts a team's players take; a block's show condition asks for one. */
  roles?: Role[];
  /** List a team's journal in the order it found the notes. */
  journal_by_unlock?: boolean;
  /** Keep the team's running score off its phone; points still count. */
  hide_score?: boolean;
  /** A walk, not a game: the phone speaks of the walk and of you, and the end has no trophy. */
  tour?: boolean;
  /** A quiz walk (made as a party adventure): the end screen lists its typed answers with its quiz picks, and new puzzles just move on. */
  quiz_puzzles?: boolean;
  /** After `seconds` from its start, the team is moved to `to` and shown `text`. */
  time_limit?: TimeLimit;
  /** Designer-only grouping boxes drawn behind steps. */
  frames?: Frame[];
  /** Looks of the adventure's own: a theme effect switches to one by slug. */
  themes?: Theme[];
  /** The saved theme players see wherever no step changes the theme; publishing makes it the adventure's look. */
  main_theme?: string;
  /** Words a party's host fills in, written into texts as {{host:<slug>}}. */
  host_values?: HostValue[];
  /** The BCP 47 tag the graph's own text is in. */
  language?: string;
  /** Other languages: language → field key → text. See i18n.ts. */
  translations?: Record<string, Record<string, string>>;
  /** The main text each translation was made from, keyed as translations. See i18n.ts. */
  translation_originals?: Record<string, Record<string, string>>;
  /** The fingerprint of the main text, for translations made before originals were kept. See i18n.ts. */
  translation_sources?: Record<string, Record<string, string>>;
  /** PDFs that come with the adventure on paper; players never see them. */
  print_materials?: PrintMaterial[];
}

/** graph.Delivery — how a print material reaches the buyer. */
export type Delivery = "posted" | "emailed" | "organiser";

/** graph.MaxPrintMaterials */
export const MAX_PRINT_MATERIALS = 10;

/** graph.PrintMaterial — one printable document, a PDF per language. */
export interface PrintMaterial {
  id: string;
  name: string;
  delivery: Delivery;
  /** Language → adventure_media id of a PDF. */
  files?: Record<string, number>;
}

/** graph.HostValue — a word a party's host fills in: the birthday child's name. */
export interface HostValue {
  slug: string;
  /** What the host page asks. */
  label: string;
  /** What everyone else, and a host who leaves it blank, sees. */
  default?: string;
}

/**
 * graph.Theme — a look saved in the adventure under a name. It replaces
 * the adventure's own look while it is on. The slug may not be a
 * built-in theme's (BUILTIN_THEMES).
 */
export interface Theme {
  slug: string;
  name: string;
  look: ThemeLook;
  /** The starter theme the look was picked from, for its cover picture. */
  starter?: string;
}

/** The built-in themes a theme effect may name: graph.Themes. */
export const BUILTIN_THEMES = ["default", "dark", "night", "alarm", "parchment"] as const;

/**
 * adventure.Colors — a whole look. The same shape as @zonventure/api's
 * play.Brand, so one passes for the other; spelled out here because
 * this package depends on nothing.
 */
export interface ThemeLook {
  accent?: string;
  accent_secondary?: string;
  background?: string;
  text?: string;
  submit_bg?: string;
  font?: string;
  heading_font?: string;
  corners?: "rounded" | "soft" | "sharp";
  cards?: "raised" | "flat";
  card_outline?: boolean;
  backdrop?: {
    kind: "dots" | "stripes" | "checks" | "grid" | "sunburst" | "waves" | "zigzag" | "confetti" | "snow" | "gradient" | "spots" | "horizon" | "lined" | "picture";
    color?: string;
    color2?: string;
    opacity: number;
    scale: number;
    angle?: number;
    fade?: boolean;
    fixed?: boolean;
    motion?: boolean;
    points?: { x: number; y: number; color?: string }[];
    colors?: string[];
    media?: number;
    blur?: number;
  };
  style?: {
    card_opacity?: number;
    card_blur?: boolean;
    transition?: "none" | "fade" | "slide" | "zoom" | "flip" | "pop";
    button_shape?: "rounded" | "square";
    button_fill?: "gradient" | "flat" | "outline" | "soft";
    button_size?: "small" | "large";
    answer_button_color?: string;
    answer_button_text_color?: string;
    dock?: "floating";
    text_scale?: number;
    title_style?: "ribbon" | "underline" | "shadow";
    title_upper?: boolean;
    celebrate?: "quiet" | "none";
    confetti_colors?: string[];
    confetti_emoji?: string;
    cheer_text?: string;
    miss_text?: string;
    sound_right?: number;
    sound_wrong?: number;
  };
}

/** graph.Frame — cosmetic, like a card's position. */
export interface Frame {
  id: string;
  title?: string;
  color?: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface TimeLimit {
  seconds: number;
  to: string;
  text?: string;
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
  switch?: SwitchConfig;
  end?: EndConfig;
  start?: StartConfig;
  /** Journal entries the team gains on reaching this step. */
  unlocks?: string[];
}

export interface Edge {
  from: string;
  from_port: string;
  to: string;
}

/** "missing": an item puzzle's way out for a team without the items. */
export type Trigger = "answer" | "timeout" | "skipped" | "attempts" | "missing";

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
  lockout_seconds?: number;
}

/** internal/graph: Media — a picture or sound, by adventure_media id. */
export interface Media {
  id: number;
  /** "video" only on a video block; steps and hints keep to pictures and sounds. */
  kind: "image" | "audio" | "video";
  alt?: string;
}

/**
 * internal/graph: Block — one piece of what a puzzle, story or end shows,
 * in the author's order. Fields are per kind; `id` is stable within the
 * step, so translations and the editor follow a block as it moves.
 */
export type Block = BlockBody & {
  id: string;
  /** Hidden until `when` holds; with tease, shown blurred meanwhile. The server enforces it. */
  show?: Show;
  /** Switched off: kept for the author, never shown. Any kind, the title and points too. */
  disabled?: boolean;
  /** How the block moves as it appears, or to catch the eye. Any kind. */
  animation?: Animation;
};

/** graph.Animation — the player's own keyframes, named as animate.style names its effects. */
export interface Animation {
  entrance?: (typeof ENTRANCES)[number];
  attention?: (typeof ATTENTIONS)[number];
  /** Seconds before the block appears: 0–10. */
  delay?: number;
  /** Seconds per run of an effect: 0.2–5; absent is 1. */
  duration?: number;
  /** How often the attention effect plays; absent is once. */
  repeat?: "three" | "forever";
}

/** graph.Entrances and graph.Attentions. */
export const ENTRANCES = ["fadeIn", "fadeInUp", "fadeInDown", "slideInLeft", "slideInRight", "zoomIn", "bounceIn", "flipInX", "backInUp", "lightSpeedInRight", "rollIn"] as const;
export const ATTENTIONS = ["pulse", "bounce", "shakeX", "wobble", "tada", "heartBeat", "swing", "rubberBand", "jello", "flash", "headShake"] as const;

export interface Show {
  when: Predicate;
  tease?: boolean;
}

/** One block's own fields, by kind. See internal/graph/blocks.go for each. */
export type BlockBody =
  /** host_edit lets a party's host rewrite the text for their party. */
  | { kind: "text"; text: string; host_edit?: boolean }
  | { kind: "image" | "video"; media: number; alt?: string }
  /** reverse lets the team play it backwards; spectrogram draws its spectrum as it plays. */
  | { kind: "audio"; media: number; alt?: string; reverse?: boolean; spectrogram?: boolean }
  /** zone is "" until chosen, for every kind that has one. */
  | { kind: "compass"; zone: string; hide_distance?: boolean }
  /** zoom fixes the zoom level (MAP_ZOOM); without it the map fits the zone and markers. */
  | { kind: "map"; zone: string; zoom?: number; hide_team?: boolean; markers?: Marker[] }
  | { kind: "note"; text: string; style?: NoteStyle }
  | { kind: "chat"; lines: ChatLine[]; media?: number; alt?: string }
  /** wheel: a Caesar cipher as a wheel the team turns, not a sheet. */
  | { kind: "cipher"; cipher: Cipher; shift?: number; key?: string; wheel?: boolean }
  | { kind: "hotspots"; media: number; alt?: string; spots?: Spot[] }
  | { kind: "divider"; style?: DividerStyle }
  | { kind: "radar"; zone: string }
  | { kind: "overlay"; media: number; alt?: string }
  /** style: how the picture is found — a torch in the dark (none), a scratch card, a UV lens, a red lens. */
  | { kind: "torch"; media: number; alt?: string; style?: TorchStyle }
  /** pitch: degrees above flat the phone must be held at; text shows then. */
  | { kind: "level"; pitch?: number; text?: string }
  /** threshold: loudness 1–100; text shows once reached. */
  | { kind: "noise"; threshold?: number; text?: string }
  | { kind: "countdown"; seconds: number; from?: "step" | "start"; text?: string }
  | { kind: "status"; items?: boolean; counters?: string[] }
  | { kind: "standings" }
  /** Where the step's title shows, and what a puzzle is worth. Always there, once: see FIXED_KINDS. */
  | { kind: "title" }
  | { kind: "points" }
  /** A quiz-walk question: three options for 1, X and 2, and the right one. Worth 0 is one point. */
  /** host_edit lets a party's host rewrite the question, its options and its answer. */
  | { kind: "quiz"; text: string; options: [string, string, string]; answer?: QuizChoice; worth?: number; host_edit?: boolean }
  /** A number question: among teams on the same score, the closest guess ranks first. */
  | { kind: "tiebreak"; text: string; target?: number }
  /** A secret in parts, one to each player's phone. */
  | { kind: "split"; parts: string[] }
  /** heading: degrees clockwise from north the phone must point at; text shows then. */
  | { kind: "bearing"; heading?: number; text?: string }
  /** An incoming call from who: answering plays the sound. */
  | { kind: "call"; media: number; alt?: string; who?: string }
  /** Suspects the team strikes out; what it struck is kept, never scored. */
  | { kind: "suspects"; cards: SuspectCard[] }
  /** A picture the team takes of text; an arranger approves it for worth points, or denies it for a retake. */
  | { kind: "photo"; text: string; worth?: number }
  /** Two pictures, media and other, and the differences to find. */
  | { kind: "spotdiff"; media: number; other?: number; alt?: string; spots?: Spot[] }
  /** text flashed in Morse at speed words a minute (default 8). */
  | { kind: "flasher"; text: string; speed?: number }
  /** text's letters as tiles to drag about. */
  | { kind: "letters"; text: string }
  /** threshold: shake 1–100; text shows once reached. */
  | { kind: "shake"; threshold?: number; text?: string }
  /** A question each player votes on from their own phone. */
  | { kind: "vote"; text: string; options: string[] }
  /** The organisers' latest message to the team. */
  | { kind: "bulletin" }
  /** Deals the team one of options at random; a show condition may ask which (op drawn). */
  | { kind: "draw"; text?: string; options: string[] }
  /** A button only the secret role's player sees and presses once: costs worth points, locks the answer for seconds, plants journal. */
  | { kind: "sabotage"; role: string; text?: string; worth?: number; seconds?: number; journal?: string }
  /** Each player names a teammate; if most name the secret role's holder, the team scores worth when it finishes. */
  | { kind: "accuse"; role: string; text: string; worth?: number }
  /** The roles: the player's own, or with team every player's; choose lets them take one here, lock makes them final once the team moves on. */
  | { kind: "roles"; text?: string; team?: boolean; choose?: boolean; lock?: boolean };

export interface SuspectCard {
  name: string;
  text?: string;
  media?: number;
  alt?: string;
}

export type TorchStyle = "scratch" | "uv" | "red";

export type QuizChoice = "1" | "X" | "2";
export const QUIZ_CHOICES: QuizChoice[] = ["1", "X", "2"];

export type NoteStyle = "paper" | "telegram" | "sms" | "newspaper" | "sticky" | "mirror" | "fact";
export type DividerStyle = "line" | "space" | "ornament";
export type Cipher = "pigpen" | "morse" | "semaphore" | "caesar" | "custom";

export interface ChatLine {
  who?: string;
  text: string;
  /** On the team's side of the conversation. */
  me?: boolean;
}

/** A hotspot: its centre as fractions of the picture, and its caption. */
export interface Spot {
  x: number;
  y: number;
  text: string;
}

export interface Marker {
  lat: number;
  lng: number;
  label?: string;
}

export type BlockKind = Block["kind"];

export interface Hint {
  text: string;
  delay_seconds?: number;
  cost?: number;
  media?: Media[];
}

export interface PuzzleConfig {
  blocks?: Block[];
  /** A party's host may set what the typed answer accepts. */
  host_answer?: boolean;
  /** Given its title and points once (graph.PuzzleConfig.Fixed); one removed since stays gone. */
  fixed?: boolean;
  zone?: string;
  outputs?: Output[];
  responses?: Response[];
  hints?: Hint[];
  time_limit_seconds?: number;
  skip_after_guesses?: number;
  skip_after_seconds?: number;
  lockout_seconds?: number;
  repeatable?: boolean;
  /** A right answer counts once every player has given one leading the same way. Only where usesMatchers. */
  unanimous?: boolean;
  /** Any answer moves on without a word of right or wrong; a wrong one by the first answer route, for no points. Only where usesMatchers, not with unanimous. */
  quiet?: boolean;
  /** The role whose player alone answers. Not with unanimous or a two-key launch. */
  by?: string;
  /** Another way of answering than typing; absent is a typed answer. */
  answer?: Answer;
  /** The answer bar's words, keyboard and QR button, for a typed answer or a scan. */
  bar?: AnswerBar;
}

/**
 * graph.AnswerBar. Placeholder and button are translated under
 * nodeKey(…, "bar:placeholder") and "bar:button"; empty is the phone's own.
 */
export interface AnswerBar {
  placeholder?: string;
  /** The answer button's label, or a scan's scan button's. */
  button?: string;
  /** Absent lets the answers decide: numbers when every one is a number. */
  keyboard?: "text" | "number";
  /** No QR button beside a typed answer. */
  no_scan?: boolean;
}

/**
 * graph.Answer: how a puzzle takes its answer, beyond typing it — one
 * kind, and only that kind's fields.
 *
 * - tiles: a picture cut into a grid and shuffled; solved when whole.
 * - search: answered by where the team stands ("We're here"). Group names
 *   the zones the phone reports the team as in; empty is the outputs' own.
 * - choice: options to pick. Ungraded (no answers), the picked value goes
 *   through the outputs' matchers; graded (every question has an answer),
 *   a form marked on submit, paying the first output's points in shares.
 * - lock: wheels turning through the same symbols; the phone sends the
 *   chosen values joined by "|" and the outputs' matchers decide.
 * - questions: answered by the step's quiz blocks; once every one shown
 *   is picked, the team continues by the first answer output.
 * - scan: a QR scan only; the decoded text goes through the matchers.
 * - items: slots the team fills from its backpack, each wanting one
 *   item; all right takes the first answer output and, unless keep,
 *   uses the items up. The phone is told the slots' labels only.
 * - keys: a two-key launch. Each player turns their own key; keys of
 *   them (every player when absent) within window seconds (10 when
 *   absent) take the first answer output — with posts, one at each;
 *   post_roles, beside posts, is the role whose player turns each ("" for
 *   anyone's).
 * - order: cards the team drags into the order they are listed in. They
 *   are dealt in start order (card indices; reversed when absent), and
 *   the phone sends the dealt positions in the team's order: "2,0,1".
 */
export type Answer =
  | { kind: "tiles"; image?: Media; rows: number; cols: number }
  | { kind: "search"; group?: string }
  | { kind: "choice"; fields: ChoiceField[] }
  | { kind: "lock"; wheels: number; symbols: LockSymbol[]; start?: number[] }
  | { kind: "questions" }
  | { kind: "scan" }
  | { kind: "items"; slots: ItemSlot[]; keep?: boolean; missing?: MissingHint }
  | { kind: "keys"; keys?: number; window?: number; posts?: string[]; post_roles?: string[] }
  | { kind: "order"; cards: OrderCard[]; start?: number[] };

/** graph.OrderCard: text on a background colour (#rrggbb), a picture, or both. */
export interface OrderCard {
  text?: string;
  color?: string;
  image?: Media;
}

/** graph.MinCards and graph.MaxCards. */
export const MIN_CARDS = 2;
export const MAX_CARDS = 8;

/**
 * graph.Missings: what an item puzzle tells a team short of an item —
 * that something is missing (absent), which items ("name"), or nothing.
 */
export type MissingHint = "name" | "none";

/** graph.ItemSlot: where an item goes, and the item it wants. */
export interface ItemSlot {
  label?: string;
  item: string;
}

/** graph.MaxSlots. */
export const MAX_SLOTS = 6;

export type AnswerKind = Answer["kind"];
/** One kind of answer, by name: AnswerOf<"lock">. */
export type AnswerOf<K extends AnswerKind> = Extract<Answer, { kind: K }>;

export interface LockSymbol {
  value: string;
  label?: string;
  image?: Media;
}
export interface ChoiceField {
  label?: string;
  options: ChoiceOption[];
  answer?: string;
}
export interface ChoiceOption {
  value: string;
  label?: string;
}

export interface StoryConfig {
  blocks?: Block[];
  /** See PuzzleConfig.fixed. */
  fixed?: boolean;
  zone?: string;
  /** Shown only the first time; a returning team passes straight through. */
  once?: boolean;
}

export type Policy = "choice" | "sequence" | "shuffle" | "spread" | "capacity" | "random";

export interface Branch {
  port: string;
  label?: string;
  /** A line under the label, and a picture on the tile. */
  note?: string;
  image?: Media;
  condition?: Predicate;
  capacity?: number;
}

export interface HubConfig {
  policy: Policy;
  branches?: Branch[];
  required?: number;
  hide_locked?: boolean;
  /** A home screen: branches stay open, back works from inside them, it never finishes. */
  home?: boolean;
  /** Shown above the branches instead of "choose where to go next". Markdown. */
  text?: string;
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
  url?: string;
  theme?: string;
  /** An email's subject and body, sent to the team's contact address. {{team}}, {{event}} and {{step}} are filled in when sent. */
  subject?: string;
  body?: string;
  /** A webhook's HTTP method, POST when absent: graph.WebhookMethods. */
  method?: WebhookMethod;
  /** Sent as given. Stored in the adventure, readable by anyone who can open or export it. */
  headers?: { name: string; value: string }[];
}

export type WebhookMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
export const WEBHOOK_METHODS: WebhookMethod[] = ["GET", "POST", "PUT", "PATCH", "DELETE"];

/**
 * internal/graph: SwitchConfig — routes on an answer. `from` names the
 * puzzle whose answer is read; empty means the latest answer, the one
 * that came down the wire. The first matching case wins; otherwise
 * "else".
 */
export interface SwitchConfig {
  from?: string;
  cases?: Case[];
}

export interface Case {
  port: string;
  label?: string;
  /** Accepted answers of a puzzle, as "node/answer-id". See answerOptions. */
  answers?: string[];
  /** Typed answers, for questions whose puzzle accepts anything. */
  matchers?: Matcher[];
}

export interface EndConfig {
  outcome?: "complete" | "fail";
  blocks?: Block[];
}

/**
 * internal/graph: Zone. A circle (lat, lng, radius_m) unless `polygon`
 * has three or more corners; then the polygon is the shape and lat/lng
 * only centres the map.
 */
export interface Zone {
  slug: string;
  name?: string;
  lat: number;
  lng: number;
  radius_m: number;
  /** Corners as [lat, lng]; the last joins the first. */
  polygon?: [number, number][];
  /** Zones that belong together, such as the cells of a grid. */
  group?: string;
  /** Keep the place from the teams: no name, map or distance on their phones, for a step where finding it is the puzzle. */
  hidden?: boolean;
  /** A party's host may put the place in their own park. */
  movable?: boolean;
}

/** internal/graph: StartConfig — where a start is, if anywhere. */
export interface StartConfig {
  zone?: string;
}

/** graph.Role: a part a player takes on a team — the navigator, the decoder. */
export interface Role {
  slug: string;
  name?: string;
  /** What the role is for, shown to the player who takes it. */
  text?: string;
  /** The role's picture, shown wherever the role is listed. */
  image?: Media;
  /** Items the role brings: the team starts with one each, and only the role's player can use them. */
  kit?: string[];
  /** Everyone else's: held by every player without another role, and by a player alone. Never dealt. */
  default?: boolean;
  /** A saboteur's: dealt at random to one player of a team of three or more, never claimed, revealed at the end. */
  secret?: boolean;
  /** A secret role's aims, each checked against the team when it finishes. */
  goals?: RoleGoal[];
}

/** graph.RoleGoal: one of a secret role's aims. */
export interface RoleGoal {
  text: string;
  when: Predicate;
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
  /** A heading notes are grouped under — a suspect, a place. */
  section?: string;
  media?: Media[];
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
  /** Stable name for this accepted answer, so a crossroads can refer to it. */
  id?: string;
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
  | "players_in_zone"
  | "zones_held"
  | "here"
  | "role"
  | "walked"
  | "drawn"
  | "time_of_day"
  | "weekday";

export type Cmp = "eq" | "ne" | "lt" | "lte" | "gt" | "gte";

export interface Predicate {
  op: Op;
  of?: Predicate[];
  node?: string;
  /** The draw block on node, for drawn; is is the card's index. */
  block?: string;
  item?: string;
  counter?: string;
  zone?: string;
  role?: string;
  cmp?: Cmp;
  n?: number;
  is?: string;
  /** Days for weekday; zones for zones_held. */
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
