import { useRef, useState } from "react";
import { play } from "@zonventure/api";
import { Markdown } from "../Markdown.tsx";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * A photo challenge: the team takes a picture with the phone's camera
 * and sends it for the organisers to review. The team moves on meanwhile;
 * an approved picture adds its points, a denied one may be taken again.
 * Photos are only taken in a real event (ctx.photo).
 */
export const PhotoBlock: Renderer<"photo"> = ({ block, ctx }) => {
  const { t } = ctx;
  const input = useRef<HTMLInputElement>(null);
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState(false);
  const photo = block.photo;
  const worth = block.worth ?? 0;
  const status = photo?.status;
  const canTake = !!ctx.photo && status !== "approved";

  const send = async (file: File | undefined) => {
    if (!file || !ctx.photo) return;
    setSending(true);
    setFailed(false);
    try {
      await ctx.photo(block.id, file);
    } catch {
      setFailed(true);
    } finally {
      setSending(false);
      if (input.current) input.current.value = "";
    }
  };

  return (
    <section className={"pv-photo" + (status ? ` is-${status}` : "")} aria-label={t("Photo challenge")}>
      <span className="pv-photo__tag">
        {t("Photo challenge")}
        {worth > 0 && <span className="pv-photo__worth">{t("+{{n}} points when approved", { n: worth })}</span>}
      </span>
      {block.text && <Markdown className="pv-photo__prompt" text={fillTeam(block.text, ctx)} />}
      {photo && <img className="pv-photo__thumb" src={play.photoURL(photo.id)} alt={t("Your photo")} />}
      <p className="pv-photo__status" role="status">
        {!ctx.photo
          ? t("Teams take the photo in a live event.")
          : sending
            ? t("Sending…")
            : failed
              ? t("The photo didn't send. Try again.")
              : status === "pending"
                ? t("Waiting for the organisers to look. You can carry on meanwhile.")
                : status === "approved"
                  ? worth > 0
                    ? t("Approved: +{{n}} points!", { n: worth })
                    : t("Approved!")
                  : status === "denied"
                    ? t("Not approved — take a new one.")
                    : t("The organisers look at it later. You can carry on meanwhile.")}
      </p>
      {status !== "approved" && (
        <>
          <button type="button" className="pv-btn pv-btn--primary" disabled={!canTake || sending || ctx.busy} onClick={() => input.current?.click()}>
            {status === "pending" ? t("Take a new photo instead") : status === "denied" ? t("Take a new photo") : t("Take photo")}
          </button>
          <input ref={input} type="file" accept="image/*" capture="environment" hidden onChange={(e) => void send(e.target.files?.[0])} />
        </>
      )}
    </section>
  );
};
