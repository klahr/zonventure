import { useState, type ReactNode } from "react";
import { play } from "@zonventure/api";
import { Markdown } from "../Markdown.tsx";
import type { Translate } from "../feedback.ts";
import { fillTeam, type Renderer } from "./context.ts";
import "./team.css";

/**
 * The roles as portraits, two or three to a row. With onChoose each is a
 * button: a free role is taken by tapping it, the player's own pick given
 * back by tapping it again; a teammate's pick cannot be tapped.
 *
 * A role nobody chose is dealt to someone all the same. Where players are
 * choosing (a roles block) that is left unsaid — it is Free until the
 * roles are final — so giving one back never seems to pick another. The
 * menu, read mid-game, says who was dealt it (dealt).
 */
export function RoleCards(props: {
  roles: play.RoleView[];
  t: Translate;
  dealt?: boolean;
  disabled?: boolean;
  onChoose?: ((role: string, claim: boolean) => Promise<void>) | undefined;
  /** The team's name, for {{team}} in a role's text. */
  teamName?: string | undefined;
}) {
  const { roles, t, dealt } = props;
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const final = roles.some((r) => r.final);

  const choose = async (role: string, claim: boolean) => {
    if (!props.onChoose) return;
    setBusy(role);
    setError(null);
    try {
      await props.onChoose(role, claim);
    } catch (err) {
      const code = (err as { code?: string }).code;
      setError(code === "role_taken"
        ? t("A teammate has chosen that role. Ask them to give it back first.")
        : code === "roles_final" ? t("Your team's roles are final now.") : t("Something went wrong. Try again."));
    } finally {
      setBusy(null);
    }
  };

  const mine = (r: play.RoleView) => !!r.mine && (r.claimed || !!dealt || !!r.secret || !!r.default || final);
  const card = (r: play.RoleView) => {
    const chosen = !!r.mine && !!r.claimed && !final;
    let under: ReactNode;
    let tap: boolean | undefined; // claim (true), give back (false), or nothing
    if (r.secret) under = t("Secret: only you know");
    else if (r.default) under = r.mine ? t("You and everyone else") : t("Everyone else");
    else if (final || r.claimed) {
      under = r.mine ? (final ? t("You") : t("Chosen")) : r.holder;
      if (chosen) tap = false;
    } else {
      under = dealt && r.holder ? `${r.mine ? t("You") : r.holder} · ${t("shared out")}` : t("Free");
      tap = true;
    }
    const can = tap !== undefined && !!props.onChoose && !final && !props.disabled;
    const name = r.name || r.slug;
    const face = (
      <>
        <span className="pv-roles__portrait">
          {r.image ? <img src={play.mediaURL(r.image.id)} alt="" /> : <span aria-hidden="true">{name.trim()[0]?.toUpperCase()}</span>}
          {chosen && <span className="pv-roles__check" aria-hidden="true">✓</span>}
        </span>
        <span className="pv-roles__name">{name}</span>
        {under && <span className="pv-roles__holder">{under}</span>}
      </>
    );
    const cls = "pv-roles__card" + (mine(r) ? " is-mine" : "") + (chosen ? " is-chosen" : "") + (r.secret ? " is-secret" : "") + (!r.mine && (r.claimed || final) ? " is-taken" : "");
    return (
      <li key={r.slug}>
        {props.onChoose && !r.secret && !r.default ? (
          <button
            type="button"
            className={cls}
            aria-pressed={chosen}
            aria-label={`${name}, ${typeof under === "string" ? under : ""}`}
            disabled={!can || busy !== null}
            onClick={() => tap !== undefined && void choose(r.slug, tap)}
          >
            {face}
          </button>
        ) : (
          <div className={cls}>{face}</div>
        )}
      </li>
    );
  };

  // What the player's own roles are for, under the portraits, so the
  // cards stay the same size.
  const told = roles.filter((r) => mine(r) && r.text);
  return (
    <>
      <ul className="pv-roles">{roles.map(card)}</ul>
      {props.onChoose && !final && !props.disabled && <p className="pv-vote__note">{t("Tap a portrait to choose it. Tap it again to let it go.")}</p>}
      {told.length > 0 && (
        <div className="pv-roles__about">
          {told.map((r) => (
            <div key={r.slug}>
              <strong>{r.name || r.slug}</strong>
              <Markdown className="pv-roles__text" text={fillTeam(r.text, { teamName: props.teamName })} />
            </div>
          ))}
        </div>
      )}
      {error && <p className="pv-vote__note pv-roles__error" role="alert">{error}</p>}
    </>
  );
}

/**
 * The roles, at a point the author picks: the player's own, or every
 * player's. With choose the players pick theirs here; once the team's
 * roles are final no one can.
 */
export const RolesBlock: Renderer<"roles"> = ({ block, ctx }) => {
  const { t } = ctx;
  const roles = ctx.roles ?? [];
  const final = roles.some((r) => r.final);
  const choosing = !!block.choose && !final && !!ctx.claimRole && !!ctx.live;

  let body;
  if (!roles.length) {
    body = <p className="pv-vote__note">{t("Each player sees their roles here on their own phone in a live event.")}</p>;
  } else if (choosing) {
    body = <RoleCards roles={roles} t={t} disabled={!!ctx.busy} onChoose={ctx.claimRole} teamName={ctx.teamName} />;
  } else if (block.team) {
    // Every player's. A secret role stays off this list, even on its own
    // player's phone: someone may be looking.
    body = <RoleCards roles={roles.filter((r) => !r.secret && (r.holder || r.default))} t={t} dealt teamName={ctx.teamName} />;
  } else {
    const mine = roles.filter((r) => r.mine);
    body = mine.length ? <RoleCards roles={mine} t={t} dealt teamName={ctx.teamName} /> : <p className="pv-vote__note">{t("You have no role.")}</p>;
  }

  return (
    <div className="pv-roles-block">
      {block.text && <Markdown text={fillTeam(block.text, ctx)} />}
      {body}
      {roles.length > 0 && (final
        ? <p className="pv-vote__note">{t("Your team's roles are final.")}</p>
        : choosing && ctx.rolesChosen === false
          ? <p className="pv-vote__note">{t("The team moves on once everyone has chosen a role.")}</p>
          : block.lock && <p className="pv-vote__note">{t("Roles are final once your team moves on.")}</p>)}
    </div>
  );
};
