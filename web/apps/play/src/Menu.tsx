import { useState, type FormEvent } from "react";
import { ApiError, play } from "@zonventure/api";
import { Sheet, type Translate } from "@zonventure/play-ui";
import type { I18n } from "@zonventure/i18n";

const LANG_NAMES: Record<string, string> = { en: "English", sv: "Svenska" };

/**
 * The player's menu: who they are on the team, the team's name, the
 * app's language, location sharing, and their data — sign out on a
 * shared phone, or erase it all. Beside the game; nothing here moves
 * the team on.
 */
export function Menu(props: {
  state: play.PlayState;
  t: Translate;
  i18n: I18n;
  usesLocation: boolean;
  shareLocation: boolean;
  onShareLocation: (on: boolean) => void;
  onRenamed: () => void;
  leave: (erase: boolean) => Promise<void>;
}) {
  const { state, t, i18n } = props;
  const [open, setOpen] = useState(false);
  const [confirmErase, setConfirmErase] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [leaving, setLeaving] = useState(false);
  const langs = i18n.available();

  const leave = async (erase: boolean) => {
    setLeaving(true);
    setError(null);
    try {
      await props.leave(erase);
    } catch {
      setError(t("Couldn't reach the server. Check your connection and try again."));
      setLeaving(false);
    }
  };

  return (
    <>
      <button type="button" className="app-inbox" onClick={() => setOpen(true)} aria-label={t("Menu")} aria-haspopup="dialog">
        <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
          <path d="M4 7h16M4 12h16M4 17h16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" fill="none" />
        </svg>
      </button>
      {open && (
        <div className="pv app-inbox__pv">
          <Sheet title={t("Menu")} t={t} onClose={() => { setOpen(false); setConfirmErase(false); }}>
            <div className="pv-sheet__body app-menu">
              <NameField
                label={t("Your name")}
                value={state.me?.name ?? ""}
                t={t}
                save={play.renameMe}
                onSaved={props.onRenamed}
                hint={t("Shown to your team and the organisers.")}
              />
              <NameField
                label={t("Team name")}
                value={state.team.name}
                t={t}
                save={play.renameTeam}
                onSaved={props.onRenamed}
                hint={t("Everyone on the team sees the change.")}
              />

              {langs.length > 1 && (
                <label className="app-menu__field">
                  <span className="app-menu__label">{t("Language")}</span>
                  <select className="pv-input" value={i18n.current()} onChange={(e) => i18n.setLocale(e.target.value)}>
                    {langs.map((l) => (
                      <option key={l} value={l}>{LANG_NAMES[l] ?? l}</option>
                    ))}
                  </select>
                </label>
              )}

              {props.usesLocation && (
                <label className="app-menu__toggle">
                  <input type="checkbox" checked={props.shareLocation} onChange={(e) => props.onShareLocation(e.target.checked)} />
                  <span>
                    <span className="app-menu__label">{t("Share location")}</span>
                    <span className="app-menu__hint">
                      {t("Off pauses your GPS pings. Gated puzzles need this on to detect arrival.")}{" "}
                      {t("It's deleted automatically after the event (within 30 days).")}
                    </span>
                  </span>
                </label>
              )}

              <div className="app-menu__account">
                <span className="app-menu__label">{t("Account")}</span>
                <button type="button" className="pv-btn pv-btn--wide" disabled={leaving} onClick={() => void leave(false)}>
                  {t("Sign out on this phone")}
                </button>
                <span className="app-menu__hint">{t("Your link still works, so you can sign in again with it.")}</span>
                {confirmErase ? (
                  <div className="app-menu__confirm" role="alert">
                    <p>{t("This wipes your location history and your answers from this event. Other team members keep playing. You'll be signed out and the link won't work again. Continue?")}</p>
                    <div className="app-menu__row">
                      <button type="button" className="pv-btn" onClick={() => setConfirmErase(false)}>{t("Cancel")}</button>
                      <button type="button" className="pv-btn app-menu__danger" disabled={leaving} onClick={() => void leave(true)}>{t("Erase")}</button>
                    </div>
                  </div>
                ) : (
                  <>
                    <button type="button" className="pv-btn pv-btn--wide app-menu__danger" onClick={() => setConfirmErase(true)}>
                      {t("Erase my play data")}
                    </button>
                    <span className="app-menu__hint">
                      {t("Permanently deletes your location pings and answer attempts from this event and revokes your link. Use at end of event or for privacy reasons.")}
                    </span>
                  </>
                )}
                {error && <p className="app-menu__error" role="alert">{error}</p>}
              </div>

              <p className="app-menu__foot">
                <a href="/privacy" target="_blank" rel="noopener">{t("Privacy")}</a>
                {" · "}
                <a href="/terms" target="_blank" rel="noopener">{t("Terms")}</a>
              </p>
            </div>
          </Sheet>
        </div>
      )}
    </>
  );
}

function NameField(props: {
  label: string;
  value: string;
  hint: string;
  t: Translate;
  save: (name: string) => Promise<unknown>;
  onSaved: () => void;
}) {
  const { t } = props;
  const [draft, setDraft] = useState(props.value);
  const [state, setState] = useState<"idle" | "saving" | "saved">("idle");
  const [error, setError] = useState<string | null>(null);
  const name = draft.trim();
  const changed = name !== "" && name !== props.value;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!changed) return;
    setState("saving");
    setError(null);
    try {
      await props.save(name);
      setState("saved");
      props.onSaved();
    } catch (err) {
      setState("idle");
      setError(
        err instanceof ApiError && err.status === 409
          ? t("Another team already has that name.")
          : err instanceof ApiError && err.status === 400
            ? t("Use 1 to 64 characters.")
            : t("Couldn't reach the server. Check your connection and try again."),
      );
    }
  };

  return (
    <form className="app-menu__field" onSubmit={(e) => void submit(e)}>
      <label className="app-menu__label" htmlFor={props.label}>{props.label}</label>
      <div className="app-menu__row">
        <input
          id={props.label}
          className="pv-input"
          value={draft}
          maxLength={64}
          autoComplete="off"
          enterKeyHint="done"
          onChange={(e) => { setDraft(e.target.value); setState("idle"); }}
        />
        <button type="submit" className="pv-btn pv-btn--primary" disabled={!changed || state === "saving"}>
          {state === "saved" && !changed ? t("Saved") : t("Save")}
        </button>
      </div>
      <span className="app-menu__hint">{error ? <span className="app-menu__error">{error}</span> : props.hint}</span>
    </form>
  );
}
