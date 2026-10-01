import { useCallback, useState, type ReactNode } from "react";
import type { play } from "@zonventure/api";
import { MediaList } from "../Media.tsx";
import { AudioFxBlock } from "./AudioFx.tsx";
import { BearingBlock } from "./Bearing.tsx";
import { BulletinBlock, DrawBlock } from "./Board.tsx";
import { CallBlock } from "./Call.tsx";
import { ChatBlock } from "./Chat.tsx";
import { CipherBlock } from "./Cipher.tsx";
import { CompassBlock } from "./Compass.tsx";
import type { BlockContext, Renderer } from "./context.ts";
import { CountdownBlock } from "./Countdown.tsx";
import { FlasherBlock } from "./Flasher.tsx";
import { HotspotsBlock } from "./Hotspots.tsx";
import { LettersBlock } from "./Letters.tsx";
import { LevelBlock } from "./Level.tsx";
import { MapBlock } from "./Map.tsx";
import { NoiseBlock } from "./Noise.tsx";
import { DividerBlock, NoteBlock } from "./Note.tsx";
import { OverlayBlock } from "./Overlay.tsx";
import { PhotoBlock } from "./Photo.tsx";
import { RadarBlock } from "./Radar.tsx";
import { ShakeBlock } from "./Shake.tsx";
import { SplitBlock } from "./Split.tsx";
import { SpotDiffBlock } from "./SpotDiff.tsx";
import { StandingsBlock } from "./Standings.tsx";
import { StatusBlock } from "./Status.tsx";
import { SuspectsBlock } from "./Suspects.tsx";
import { Teaser } from "./Teaser.tsx";
import { PointsBlock, TextBlock, TitleBlock } from "./Text.tsx";
import { QuizBlock, TiebreakBlock } from "./Quiz.tsx";
import { TorchBlock } from "./Torch.tsx";
import { VideoBlock } from "./Video.tsx";
import { VoteBlock } from "./Vote.tsx";
import { AccuseBlock, SabotageBlock } from "./Saboteur.tsx";
import { RolesBlock } from "./Roles.tsx";

type Kind = play.PlayBlock["kind"];

/**
 * How each kind of block is drawn. A new kind is a file here and a line
 * below; TypeScript refuses the build until both exist. Pictures and
 * plain sounds are drawn by Blocks itself, grouped; a sound with tricks
 * (hasFx) is drawn on its own.
 */
const RENDER: { [K in Exclude<Kind, "image" | "audio">]: Renderer<K> } = {
  text: TextBlock,
  video: VideoBlock,
  compass: ({ block, ctx }) => <CompassBlock block={block} ctx={ctx} />,
  map: MapBlock,
  note: NoteBlock,
  chat: ChatBlock,
  cipher: CipherBlock,
  hotspots: HotspotsBlock,
  divider: DividerBlock,
  radar: RadarBlock,
  overlay: OverlayBlock,
  level: LevelBlock,
  noise: NoiseBlock,
  torch: TorchBlock,
  countdown: CountdownBlock,
  status: StatusBlock,
  standings: StandingsBlock,
  title: TitleBlock,
  points: PointsBlock,
  quiz: QuizBlock,
  tiebreak: TiebreakBlock,
  split: SplitBlock,
  bearing: BearingBlock,
  call: CallBlock,
  suspects: SuspectsBlock,
  photo: PhotoBlock,
  spotdiff: SpotDiffBlock,
  flasher: FlasherBlock,
  letters: LettersBlock,
  shake: ShakeBlock,
  vote: VoteBlock,
  sabotage: SabotageBlock,
  accuse: AccuseBlock,
  roles: RolesBlock,
  bulletin: BulletinBlock,
  draw: DrawBlock,
};

/** A sound played backwards or drawn as a spectrogram: it needs its own player. */
const hasFx = (b: play.PlayBlock) => b.kind === "audio" && !!(b.reverse || b.spectrogram);

export interface BlocksProps extends BlockContext {
  blocks: play.PlayBlock[] | undefined;
}

/**
 * A puzzle's, story's or end's blocks, in the author's order. Pictures
 * and sounds next to each other share one media group, laid out as a
 * step's media were before blocks. A block of a kind this bundle does
 * not know, from a newer server, is skipped. What follows a chat waits
 * until its last message is in, as the next thing said in a chat would.
 */
export function Blocks({ blocks, ...rest }: BlocksProps) {
  const [done, setDone] = useState<ReadonlySet<string>>(() => new Set());
  const played = useCallback((id: string) => setDone((d) => (d.has(id) ? d : new Set(d).add(id))), []);
  if (!blocks?.length) return null;
  const ctx: BlockContext = { ...rest, played };
  const out: ReactNode[] = [];
  let run: play.PlayMedia[] = [];
  const flush = () => {
    if (run.length) out.push(<MediaList key={`media-${out.length}`} media={run} />);
    run = [];
  };
  for (const b of blocks) {
    if (!b.locked && (b.kind === "image" || b.kind === "audio") && !hasFx(b)) {
      run.push({ id: b.media, kind: b.kind, ...(b.alt ? { alt: b.alt } : {}) });
      continue;
    }
    flush();
    if (b.locked) {
      out.push(<Teaser key={b.id} block={b} ctx={ctx} />);
      continue;
    }
    const Render = (b.kind === "audio" ? AudioFxBlock : RENDER[b.kind as keyof typeof RENDER]) as Renderer<Kind> | undefined;
    if (Render) out.push(<Render key={b.id} block={b} ctx={ctx} />);
    if (b.kind === "chat" && !done.has(b.id)) break;
  }
  flush();
  return <>{out}</>;
}
