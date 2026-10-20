import type { BlockKind } from "@zonventure/graph";
import {
  Activity,
  AlarmClock,
  Bomb,
  Gavel,
  Camera,
  Handshake,
  Compass,
  Crosshair,
  Dices,
  Diff,
  Film,
  Flashlight,
  Heading,
  IdCard,
  ListChecks,
  ListTodo,
  Image,
  KeyRound,
  Map as MapIcon,
  Megaphone,
  MessageSquare,
  Minus,
  MousePointerClick,
  Navigation,
  Package,
  PhoneIncoming,
  Puzzle,
  Radar,
  Scale,
  ScanEye,
  Siren,
  Smartphone,
  Star,
  StickyNote,
  Trophy,
  Type,
  Users,
  UserSearch,
  Volume2,
  Vote,
} from "lucide-react";
import { BearingEditor } from "./Bearing.tsx";
import { ChatEditor } from "./Chat.tsx";
import { CipherEditor, FlasherEditor, LettersEditor } from "./Cipher.tsx";
import { CountdownEditor, StandingsEditor, StatusEditor } from "./Game.tsx";
import { HotspotsEditor, SpotDiffEditor } from "./Hotspots.tsx";
import { LevelEditor } from "./Level.tsx";
import { AudioEditor, CallEditor, ImageEditor, VideoEditor } from "./Media.tsx";
import { NoiseEditor, ShakeEditor } from "./Noise.tsx";
import { DividerEditor, NoteEditor } from "./Note.tsx";
import { OverlayEditor } from "./Overlay.tsx";
import { CompassEditor, MapEditor, RadarEditor } from "./Place.tsx";
import { PointsEditor, TextEditor, TitleEditor } from "./Text.tsx";
import { QuizEditor, TiebreakEditor } from "./Quiz.tsx";
import { AccuseEditor, BulletinEditor, RolesEditor, DrawEditor, MissionEditor, PhotoEditor, SabotageEditor, SplitEditor, TaskEditor, VoteEditor } from "./Social.tsx";
import { SuspectsEditor } from "./Suspects.tsx";
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
  call: { label: "Incoming call", icon: PhoneIncoming, group: "Content", about: "The phone rings; answering plays a sound", editor: CallEditor },
  compass: { label: "Compass", icon: Compass, group: "Place", about: "Points at a zone", editor: CompassEditor },
  map: { label: "Map", icon: MapIcon, group: "Place", about: "A zone on a map, with markers", editor: MapEditor },
  radar: { label: "Hot and cold", icon: Radar, group: "Place", about: "Warmer as the team gets closer", editor: RadarEditor },
  chat: { label: "Chat", icon: MessageSquare, group: "Props", about: "An intercepted conversation", editor: ChatEditor },
  cipher: { label: "Cipher sheet", icon: KeyRound, group: "Props", about: "A key for decoding: Morse, pigpen, …", editor: CipherEditor },
  hotspots: { label: "Hotspots", icon: MousePointerClick, group: "Props", about: "A picture with places to tap", editor: HotspotsEditor },
  torch: { label: "Torch", icon: Flashlight, group: "Props", about: "A hidden picture: torch, scratch card, UV or red lens", editor: TorchEditor },
  suspects: { label: "Suspect board", icon: UserSearch, group: "Props", about: "Suspects the team strikes out", editor: SuspectsEditor },
  spotdiff: { label: "Spot the difference", icon: Diff, group: "Props", about: "Two pictures, and the differences to tap", editor: SpotDiffEditor },
  letters: { label: "Letter tiles", icon: Puzzle, group: "Props", about: "Letters to drag about for an anagram", editor: LettersEditor },
  overlay: { label: "Look-through", icon: ScanEye, group: "Gadgets", about: "An old photo over the camera", editor: OverlayEditor },
  level: { label: "Level", icon: Crosshair, group: "Gadgets", about: "Hold the phone at an angle", editor: LevelEditor },
  noise: { label: "Noise meter", icon: Activity, group: "Gadgets", about: "Make enough noise", editor: NoiseEditor },
  bearing: { label: "Bearing lock", icon: Navigation, group: "Gadgets", about: "Point the phone the right way", editor: BearingEditor },
  shake: { label: "Shake to reveal", icon: Smartphone, group: "Gadgets", about: "Shake the phone hard enough", editor: ShakeEditor },
  flasher: { label: "Morse flasher", icon: Siren, group: "Gadgets", about: "A message flashed in Morse", editor: FlasherEditor },
  countdown: { label: "Countdown", icon: AlarmClock, group: "Game", about: "Time running out", editor: CountdownEditor },
  status: { label: "Status", icon: Package, group: "Game", about: "What the team carries, and counters", editor: StatusEditor },
  standings: { label: "Standings", icon: Trophy, group: "Game", about: "The team's place in the event", editor: StandingsEditor },
  split: { label: "Split secret", icon: Users, group: "Game", about: "A secret shared out across the players' phones", editor: SplitEditor },
  vote: { label: "Team vote", icon: Vote, group: "Game", about: "Each player votes on their own phone", editor: VoteEditor },
  photo: { label: "Photo challenge", icon: Camera, group: "Game", about: "A picture for the arranger to approve", editor: PhotoEditor },
  draw: { label: "Random draw", icon: Dices, group: "Game", about: "Deal the team a card at random", editor: DrawEditor },
  bulletin: { label: "Live bulletin", icon: Megaphone, group: "Game", about: "The organisers' latest message", editor: BulletinEditor },
  sabotage: { label: "Sabotage", icon: Bomb, group: "Game", about: "A button only the saboteur sees", editor: SabotageEditor },
  accuse: { label: "Accusation", icon: Gavel, group: "Game", about: "The players name the saboteur", editor: AccuseEditor },
  task: { label: "Task", icon: ListTodo, group: "Game", about: "One role's own part of a step done in parallel", editor: TaskEditor },
  mission: { label: "Mission", icon: Handshake, group: "Game", about: "A secret pick at a post: keep faith or betray", editor: MissionEditor },
  roles: { label: "Roles", icon: IdCard, group: "Game", about: "The players' roles, and a choice of them", editor: RolesEditor },
  quiz: { label: "Quiz question", icon: ListChecks, group: "Quiz", about: "1, X or 2, marked at the end", editor: QuizEditor },
  tiebreak: { label: "Tie-breaker", icon: Scale, group: "Quiz", about: "A number question that breaks ties", editor: TiebreakEditor },
  // Always on the step, never in the add menu: see FIXED_KINDS.
  title: { label: "Title", icon: Heading, group: "Content", about: "The step's title", editor: TitleEditor },
  points: { label: "Points", icon: Star, group: "Game", about: "What the puzzle is worth", editor: PointsEditor },
};

export const GROUPS: Group[] = ["Content", "Quiz", "Place", "Props", "Gadgets", "Game"];
