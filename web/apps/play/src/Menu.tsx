import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, play } from "@zonventure/api";
import type { Translate } from "@zonventure/play-ui";
import type { I18n } from "@zonventure/i18n";

const LANG_NAMES: Record<string, string> = { en: "English", sv: "Svenska" };

/**
 * The player's menu: who they are on the team, the team's name, the
 * app's language, location sharing, and their data — sign out on a
 * shared phone, or erase it all. Beside the game; nothing here moves
 * the team on.
 *
 * A drawer that folds in from the left edge. The ☰ at the bar's left
 * end turns into the ✕ that closes it and stays above the drawer, so
 * open and close are the same place under the thumb.
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
  // Each opening starts from the current names, not an old draft.
  const [opened, setOpened] = useState(0);
  // Which account action is waiting for a yes: both end the game on
  // this phone, so neither happens on a single tap.
  const [confirm, setConfirm] = useState<"signout" | "erase" | null>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [error, setError] = useState<string | null>(null);
  const [leaving, setLeaving] = useState(false);
  const langs = i18n.available();

  const leave = async (erase: boolean) => {
    setLeaving(true);
    setError(null);
    try {
      await props.leave(erase);
    } catch (err) {
      setError(err instanceof ApiError ? t("Something went wrong. Try again.") : t("Couldn't reach the server. Check your connection and try again."));
      setLeaving(false);
    }
  };

  const close = () => {
    setOpen(false);
    setConfirm(null);
    button.current?.focus();
  };
  const toggle = () => {
    if (open) {
      close();
      return;
    }
    setOpened((n) => n + 1);
    setOpen(true);
  };

  // Escape closes; focus goes into the drawer, and Tab stays within the
  // drawer and its ✕ while it is open.
  useEffect(() => {
    if (!open) return;
    // The drawer itself, not its first field: focusing an input would
    // pop the phone's keyboard over the menu the player just opened.
    panel.current?.focus({ preventScroll: true });
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        close();
        return;
      }
      if (e.key !== "Tab" || !panel.current || !button.current) return;
      const items = [button.current, ...panel.current.querySelectorAll<HTMLElement>("input, select, button:not(:disabled), a[href]")];
      const i = items.indexOf(document.activeElement as HTMLElement);
      const next = e.shiftKey ? (i <= 0 ? items.length - 1 : i - 1) : (i === items.length - 1 ? 0 : i + 1);
      items[next]?.focus();
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <>
      <button
        ref={button}
        type="button"
        className={"app-burger" + (open ? " is-open" : "")}
        onClick={toggle}
        aria-label={open ? t("Close menu") : t("Menu")}
        aria-expanded={open}
        aria-controls="app-menu"
      >
        <span className="app-burger__lines" aria-hidden="true">
          <span />
          <span />
          <span />
        </span>
      </button>
      <div className={"app-drawer-back" + (open ? " is-open" : "")} onClick={close} aria-hidden="true" />
      <div
        id="app-menu"
        ref={panel}
        className={"app-drawer" + (open ? " is-open" : "")}
        role="dialog"
        aria-modal="true"
        aria-label={t("Menu")}
        tabIndex={-1}
        inert={!open}
      >
            <h2 className="app-drawer__title">{t("Menu")}</h2>
            <div className="app-menu" key={opened}>
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
                {confirm === "signout" ? (
                  <div className="app-menu__confirm app-menu__confirm--calm" role="alert">
                    <p>{t("Sign out on this phone? You'll need your link to get back into the game.")}</p>
                    <div className="app-menu__row">
                      <button type="button" className="pv-btn" onClick={() => setConfirm(null)}>{t("Cancel")}</button>
                      <button type="button" className="pv-btn pv-btn--primary" disabled={leaving} onClick={() => void leave(false)}>{t("Sign out")}</button>
                    </div>
                  </div>
                ) : (
                  <>
                    <button type="button" className="pv-btn pv-btn--wide" onClick={() => setConfirm("signout")}>
                      {t("Sign out on this phone")}
                    </button>
                    <span className="app-menu__hint">{t("Your link still works, so you can sign in again with it.")}</span>
                  </>
                )}
                {confirm === "erase" ? (
                  <div className="app-menu__confirm" role="alert">
                    <p>{t("This wipes your location history and your answers from this event. Other team members keep playing. You'll be signed out and the link won't work again. Continue?")}</p>
                    <div className="app-menu__row">
                      <button type="button" className="pv-btn" onClick={() => setConfirm(null)}>{t("Cancel")}</button>
                      <button type="button" className="pv-btn app-menu__danger" disabled={leaving} onClick={() => void leave(true)}>{t("Erase")}</button>
                    </div>
                  </div>
                ) : (
                  <>
                    <button type="button" className="pv-btn pv-btn--wide app-menu__danger" onClick={() => setConfirm("erase")}>
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
      </div>
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
