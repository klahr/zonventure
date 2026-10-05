import { useState } from "react";
import { personalise, placeAt, setMode, shiftTo, type BlockOverride, type Party } from "./api.ts";
import { useT } from "./i18n.tsx";

type Run = (fn: () => Promise<unknown>) => Promise<void>;

/** Where the party is played: cards anywhere, or places in a park. */
export function WhereSection({ token, party, run, busy }: { token: string; party: Party; run: Run; busy: boolean }) {
  const t = useT().t;
  const editable = party.state === "scheduled" && !party.expired;
  const choice = (mode: "qr" | "park", title: string, body: string, ok: boolean) => (
    <label className={"party-choice" + (party.mode === mode ? " is-on" : "")}>
      <input type="radio" name="mode" checked={party.mode === mode} disabled={!editable || busy || !ok} onChange={() => void run(() => setMode(token, mode))} />
      <span>
        <strong>{title}</strong>
        <small>{ok ? body : t("This adventure can't be played this way.")}</small>
      </span>
    </label>
  );
  return (
    <div className="party-choices" role="radiogroup" aria-label={t("Where are you playing?")}>
      {choice("qr", t("At home or in the garden"), t("You print cards and hide them; the teams scan them. No GPS needed."), party.cards_ok)}
      {choice("park", t("In a park"), t("You put each place where you want it, standing there with your phone; the teams walk to them."), party.park_ok)}
    </div>
  );
}

/**
 * The host's words: the values the adventure asks for, and the texts,
 * questions and answers its author let them rewrite.
 */
export function PersonaliseSection({ token, party, run, busy }: { token: string; party: Party; run: Run; busy: boolean }) {
  const t = useT().t;
  const editable = party.state === "scheduled" && !party.expired;
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(party.values.map((v) => [v.slug, v.value])));
  const [blocks, setBlocks] = useState<Record<string, BlockOverride>>(() =>
    Object.fromEntries(party.editables.filter((e) => e.kind).map((e) => [e.key, e.mine ?? { text: e.text ?? "", options: e.options ?? [], answer: e.answer ?? "" }])),
  );
  const [answers, setAnswers] = useState<Record<string, string>>(() =>
    Object.fromEntries(party.editables.filter((e) => !e.kind).map((e) => [e.key, (e.my_accepts ?? e.accepts ?? []).join(", ")])),
  );
  const [saved, setSaved] = useState(false);
  const setBlock = (key: string, b: BlockOverride) => {
    setSaved(false);
    setBlocks((all) => ({ ...all, [key]: b }));
  };

  const save = () =>
    run(async () => {
      await personalise(token, {
        values,
        blocks,
        answers: Object.fromEntries(Object.entries(answers).map(([k, v]) => [k, v.split(",").map((s) => s.trim()).filter(Boolean)])),
      });
      setSaved(true);
    });

  return (
    <form
      className="party-form"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      {party.values.map((v) => (
        <label className="party-field" key={v.slug}>
          <span>{v.label}</span>
          <input
            value={values[v.slug] ?? ""}
            maxLength={80}
            placeholder={v.default}
            disabled={!editable}
            onChange={(e) => {
              setSaved(false);
              setValues((all) => ({ ...all, [v.slug]: e.target.value }));
            }}
          />
        </label>
      ))}
      {party.editables.map((e) => {
        if (!e.kind) {
          return (
            <label className="party-field" key={e.key}>
              <span>{t("Accepted answers in “{{step}}”", { step: e.step })}</span>
              <input
                value={answers[e.key] ?? ""}
                disabled={!editable}
                onChange={(ev) => {
                  setSaved(false);
                  setAnswers((all) => ({ ...all, [e.key]: ev.target.value }));
                }}
              />
              <small className="party-muted">{t("Separate several with commas.")}</small>
            </label>
          );
        }
        const b = blocks[e.key] ?? {};
        if (e.kind === "text") {
          return (
            <label className="party-field" key={e.key}>
              <span>{t("Text in “{{step}}”", { step: e.step })}</span>
              <textarea rows={3} value={b.text ?? ""} disabled={!editable} onChange={(ev) => setBlock(e.key, { ...b, text: ev.target.value })} />
            </label>
          );
        }
        const opts = b.options?.length === 3 ? b.options : ["", "", ""];
        return (
          <fieldset className="party-quiz" key={e.key} disabled={!editable}>
            <legend>{t("Question in “{{step}}”", { step: e.step })}</legend>
            <input value={b.text ?? ""} aria-label={t("Question")} onChange={(ev) => setBlock(e.key, { ...b, text: ev.target.value })} />
            {(["1", "X", "2"] as const).map((c, i) => (
              <label className="party-quiz__opt" key={c}>
                <input type="radio" name={"right-" + e.key} checked={b.answer === c} onChange={() => setBlock(e.key, { ...b, options: opts, answer: c })} aria-label={t("Right answer")} />
                <span className="party-quiz__mark">{c}</span>
                <input
                  value={opts[i] ?? ""}
                  aria-label={t("Option {{c}}", { c })}
                  onChange={(ev) => setBlock(e.key, { ...b, options: opts.map((o, j) => (j === i ? ev.target.value : o)) })}
                />
              </label>
            ))}
            <small className="party-muted">{t("Tick the right answer.")}</small>
          </fieldset>
        );
      })}
      {editable && (
        <div className="party-actions">
          <button className="party-btn party-btn--primary" disabled={busy}>
            {t("Save")}
          </button>
          {saved && <span className="party-muted">{t("Saved. The game uses your words.")}</span>}
        </div>
      )}
    </form>
  );
}

/** Where the host is standing, as precisely as the phone can tell. */
function whereAmI(): Promise<GeolocationPosition> {
  return new Promise((resolve, reject) => {
    if (!("geolocation" in navigator)) {
      reject(new Error("no geolocation"));
      return;
    }
    navigator.geolocation.getCurrentPosition(resolve, reject, { enableHighAccuracy: true, timeout: 20_000, maximumAge: 0 });
  });
}

const mapURL = (lat: number, lng: number) => `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=18/${lat}/${lng}`;

/**
 * Putting the adventure's places in the host's park: walk to each spot
 * and tap, or move them all at once from where the host stands.
 */
export function PlacesSection({ token, party, run, busy }: { token: string; party: Party; run: Run; busy: boolean }) {
  const t = useT().t;
  const [note, setNote] = useState<string | null>(null);
  const editable = party.state === "scheduled" && !party.expired;
  const here = (fn: (lat: number, lng: number) => Promise<unknown>) =>
    run(async () => {
      setNote(t("Finding where you are…"));
      try {
        const pos = await whereAmI();
        await fn(pos.coords.latitude, pos.coords.longitude);
        setNote(pos.coords.accuracy > 30 ? t("Placed, but your phone was only sure to about {{m}} m. Try again in the open for a better fix.", { m: Math.round(pos.coords.accuracy) }) : null);
      } catch (e) {
        if (e instanceof GeolocationPositionError || (e instanceof Error && e.message === "no geolocation")) {
          setNote(t("Your phone didn't say where you are. Allow location for this page and try again."));
          return;
        }
        setNote(null);
        throw e;
      }
    });
  const placed = party.places.filter((p) => p.movable && p.placed).length;
  const movable = party.places.filter((p) => p.movable).length;
  return (
    <>
      <p className="party-note">{t("Choose a park without traffic, where the children can be watched. Each place is a circle of at least 25 metres: phones aren't more exact than that.")}</p>
      {editable && (
        <>
          <p className="party-muted">{t("Walk to each spot and tap “Put it here”. Or stand in the middle of your park and move all the places at once, keeping the adventure's layout — then check each one.")}</p>
          <button className="party-btn" disabled={busy} onClick={() => void here((lat, lng) => shiftTo(token, lat, lng))}>
            {t("Move all places to around me")}
          </button>
        </>
      )}
      {note && (
        <p className="party-muted" role="status">
          {note}
        </p>
      )}
      <ul className="party-places">
        {party.places.map((p) => (
          <li key={p.slug}>
            <div>
              <strong>{p.name}</strong>
              <span className="party-muted">
                {!p.movable ? t("Stays where the author put it") : p.placed ? t("Placed") : t("Not placed yet")}
                {p.placed && (
                  <>
                    {" · "}
                    <a href={mapURL(p.lat, p.lng)} target="_blank" rel="noopener noreferrer">
                      {t("Map")}
                    </a>
                  </>
                )}
              </span>
            </div>
            {editable && p.movable && (
              <button className="party-btn" disabled={busy} onClick={() => void here((lat, lng) => placeAt(token, p.slug, lat, lng))}>
                {t("Put it here")}
              </button>
            )}
          </li>
        ))}
      </ul>
      <p className="party-muted">{t("{{placed}} of {{total}} places put.", { placed, total: movable })}</p>
    </>
  );
}
