import { useEffect, useRef, useState } from "react";
import { play } from "@zonventure/api";
import { formatSeconds } from "../clock.ts";
import type { Renderer } from "./context.ts";
import "./team.css";

type Phase = "ringing" | "on" | "ended" | "missed";

/**
 * An incoming call: the phone rings on screen until the team answers,
 * which plays the author's sound, or declines, which leaves a missed call
 * to ring back. Nothing plays by itself.
 */
export const CallBlock: Renderer<"call"> = ({ block, ctx }) => {
  const { t } = ctx;
  const who = block.who?.trim() || t("Unknown caller");
  const [phase, setPhase] = useState<Phase>("ringing");
  const [secs, setSecs] = useState(0);
  const audio = useRef<HTMLAudioElement>(null);

  useEffect(() => {
    const a = audio.current;
    if (!a) return;
    const tick = () => setSecs(Math.floor(a.currentTime));
    const end = () => setPhase("ended");
    a.addEventListener("timeupdate", tick);
    a.addEventListener("ended", end);
    return () => {
      a.removeEventListener("timeupdate", tick);
      a.removeEventListener("ended", end);
    };
  }, []);

  const answer = () => {
    const a = audio.current;
    if (!a) return;
    a.currentTime = 0;
    setSecs(0);
    setPhase("on");
    void a.play().catch(() => setPhase("ringing"));
  };
  const hangUp = () => {
    audio.current?.pause();
    setPhase("ended");
  };

  return (
    <section className={`pv-call pv-call--${phase}`} aria-label={t("Call from {{who}}", { who })}>
      <span className="pv-call__avatar" aria-hidden="true">
        {initials(who)}
      </span>
      <div className="pv-call__who">
        <span className="pv-call__name">{who}</span>
        <span className="pv-call__state" role="status">
          {phase === "ringing"
            ? t("Incoming call…")
            : phase === "on"
              ? formatSeconds(secs)
              : phase === "missed"
                ? t("Missed call")
                : t("Call ended")}
        </span>
      </div>
      <div className="pv-call__actions">
        {phase === "ringing" && (
          <>
            <button type="button" className="pv-call__btn pv-call__btn--decline" onClick={() => setPhase("missed")} disabled={ctx.preview} aria-label={t("Decline")}>
              ✕
            </button>
            <button type="button" className="pv-call__btn pv-call__btn--answer" onClick={answer} disabled={ctx.preview} aria-label={t("Answer")}>
              ✆
            </button>
          </>
        )}
        {phase === "on" && (
          <button type="button" className="pv-call__btn pv-call__btn--decline" onClick={hangUp} aria-label={t("Hang up")}>
            ✕
          </button>
        )}
        {(phase === "missed" || phase === "ended") && (
          <button type="button" className="pv-btn" onClick={answer}>
            {phase === "missed" ? t("Call back") : t("Listen again")}
          </button>
        )}
      </div>
      <audio ref={audio} preload="none" src={play.mediaURL(block.media)} aria-label={block.alt || undefined} />
    </section>
  );
};

/** Up to two initials for the avatar: "Anna Berg" is AB. */
function initials(name: string): string {
  const words = name.split(/\s+/).filter(Boolean);
  return (words.length > 1 ? words[0]![0]! + words[words.length - 1]![0]! : (words[0] ?? "?").slice(0, 2)).toUpperCase();
}
