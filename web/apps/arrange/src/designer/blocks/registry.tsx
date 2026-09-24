import type { BlockKind } from "@zonventure/graph";
import {
  Activity,
  AlarmClock,
  Compass,
  Crosshair,
  Film,
  Flashlight,
  Heading,
  ListChecks,
  Image,
  KeyRound,
  Map as MapIcon,
  MessageSquare,
  Minus,
  MousePointerClick,
  Package,
  Radar,
  Scale,
  ScanEye,
  Star,
  StickyNote,
  Trophy,
  Type,
  Volume2,
} from "lucide-react";
import { ChatEditor } from "./Chat.tsx";
import { CipherEditor } from "./Cipher.tsx";
import { CountdownEditor, StandingsEditor, StatusEditor } from "./Game.tsx";
import { HotspotsEditor } from "./Hotspots.tsx";
import { LevelEditor } from "./Level.tsx";
import { AudioEditor, ImageEditor, VideoEditor } from "./Media.tsx";
import { NoiseEditor } from "./Noise.tsx";
import { DividerEditor, NoteEditor } from "./Note.tsx";
import { OverlayEditor } from "./Overlay.tsx";
import { CompassEditor, MapEditor, RadarEditor } from "./Place.tsx";
import { PointsEditor, TextEditor, TitleEditor } from "./Text.tsx";
import { QuizEditor, TiebreakEditor } from "./Quiz.tsx";
import { TorchEditor } from "./Torch.tsx";
import type { Group, KindDef } from "./types.ts";

/**
 * Every kind of block and how it is edited. A new kind is a type in
 * @zonventure/graph, a file here with its editor, a line below, and a
 * renderer in play-ui's blocks/; TypeScript refuses the build until all
 * are there.
 */
export const KINDS: { [K in BlockKind]: KindDef<K> } = {
  text: { label: "Text", icon: Type, group: "Content", about: "Words the team reads", editor: TextEditor },
  note: { label: "Note", icon: StickyNote, group: "Content", about: "Text as a paper note, telegram or text message", editor: NoteEditor },
  image: { label: "Picture", icon: Image, group: "Content", about: "A photo or drawing", editor: ImageEditor },
  audio: { label: "Sound", icon: Volume2, group: "Content", about: "A clip to play", editor: AudioEditor },
  video: { label: "Video", icon: Film, group: "Content", about: "A short film", editor: VideoEditor },
  divider: { label: "Divider", icon: Minus, group: "Content", about: "A line or a pause between blocks", editor: DividerEditor },
  compass: { label: "Compass", icon: Compass, group: "Place", about: "Points at a zone", editor: CompassEditor },
  map: { label: "Map", icon: MapIcon, group: "Place", about: "A zone on a map, with markers", editor: MapEditor },
  radar: { label: "Hot and cold", icon: Radar, group: "Place", about: "Warmer as the team gets closer", editor: RadarEditor },
  chat: { label: "Chat", icon: MessageSquare, group: "Props", about: "An intercepted conversation", editor: ChatEditor },
  cipher: { label: "Cipher sheet", icon: KeyRound, group: "Props", about: "A key for decoding: Morse, pigpen, …", editor: CipherEditor },
  hotspots: { label: "Hotspots", icon: MousePointerClick, group: "Props", about: "A picture with places to tap", editor: HotspotsEditor },
  torch: { label: "Torch", icon: Flashlight, group: "Props", about: "A dark picture to explore with a light", editor: TorchEditor },
  overlay: { label: "Look-through", icon: ScanEye, group: "Gadgets", about: "An old photo over the camera", editor: OverlayEditor },
  level: { label: "Level", icon: Crosshair, group: "Gadgets", about: "Hold the phone at an angle", editor: LevelEditor },
  noise: { label: "Noise meter", icon: Activity, group: "Gadgets", about: "Make enough noise", editor: NoiseEditor },
  countdown: { label: "Countdown", icon: AlarmClock, group: "Game", about: "Time running out", editor: CountdownEditor },
  status: { label: "Status", icon: Package, group: "Game", about: "What the team carries, and counters", editor: StatusEditor },
  standings: { label: "Standings", icon: Trophy, group: "Game", about: "The team's place in the event", editor: StandingsEditor },
  quiz: { label: "Quiz question", icon: ListChecks, group: "Quiz", about: "1, X or 2, marked at the end", editor: QuizEditor },
  tiebreak: { label: "Tie-breaker", icon: Scale, group: "Quiz", about: "A number question that breaks ties", editor: TiebreakEditor },
  // Always on the step, never in the add menu: see FIXED_KINDS.
  title: { label: "Title", icon: Heading, group: "Content", about: "The step's title", editor: TitleEditor },
  points: { label: "Points", icon: Star, group: "Game", about: "What the puzzle is worth", editor: PointsEditor },
};

export const GROUPS: Group[] = ["Content", "Quiz", "Place", "Props", "Gadgets", "Game"];
