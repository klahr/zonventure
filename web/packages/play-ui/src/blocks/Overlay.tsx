import { useEffect, useRef, useState } from "react";
import { play } from "@zonventure/api";
import type { Renderer } from "./context.ts";
import "./overlay.css";

type Camera = "off" | "starting" | "on" | "failed";

/**
 * An old photo laid over the live camera, for lining it up with the
 * street. The camera starts only on a tap (iOS asks for a gesture) and
 * every track stops on stop or unmount, so the light goes out. A preview
 * never asks for the camera.
 */
export const OverlayBlock: Renderer<"overlay"> = ({ block, ctx }) => {
  const [camera, setCamera] = useState<Camera>("off");
  const [opacity, setOpacity] = useState(50);
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const alive = useRef(true);
  const src = play.mediaURL(block.media);

  const stop = () => {
    stream.current?.getTracks().forEach((t) => t.stop());
    stream.current = null;
  };
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      stop();
    };
  }, []);
  useEffect(() => {
    if (camera === "on" && video.current) video.current.srcObject = stream.current;
  }, [camera]);

  const start = async () => {
    if (!navigator.mediaDevices?.getUserMedia) return setCamera("failed");
    setCamera("starting");
    try {
      const s = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false });
      // Left the step while the phone was asking: let the camera go at once.
      if (!alive.current) return s.getTracks().forEach((t) => t.stop());
      stream.current = s;
      setCamera("on");
    } catch {
      if (alive.current) setCamera("failed");
    }
  };

  const layered = ctx.preview || camera === "on";
  return (
    <figure className="pv-overlay">
      <div className={"pv-overlay__stage" + (layered ? " is-layered" : "") + (ctx.preview ? " is-preview" : "")}>
        {!ctx.preview && camera === "on" && <video ref={video} className="pv-overlay__video" playsInline muted autoPlay />}
        <img className="pv-overlay__img" src={src} alt={block.alt ?? ""} style={layered ? { opacity: opacity / 100 } : undefined} />
      </div>
      {layered && (
        <label className="pv-overlay__fade">
          <span>{ctx.t("Picture")}</span>
          <input type="range" min={0} max={100} value={opacity} onChange={(e) => setOpacity(Number(e.target.value))} aria-valuetext={`${opacity}%`} />
          <span className="pv-overlay__pct">{opacity}%</span>
        </label>
      )}
      {camera === "failed" && <p className="pv-muted">{ctx.t("The camera is not available, so here is the picture on its own.")}</p>}
      {!ctx.preview && camera === "on" && (
        <button
          type="button"
          className="pv-btn"
          onClick={() => {
            stop();
            setCamera("off");
          }}
        >
          {ctx.t("Stop camera")}
        </button>
      )}
      {!ctx.preview && (camera === "off" || camera === "starting") && (
        <button type="button" className="pv-btn pv-btn--primary" onClick={start} disabled={camera === "starting"}>
          {ctx.t("Start camera")}
        </button>
      )}
      {!ctx.preview && camera === "off" && <p className="pv-muted">{ctx.t("Hold the old picture over the street through your camera and line them up.")}</p>}
    </figure>
  );
};
