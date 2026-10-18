import { useState } from "react";
import { Check, ChevronDown, ChevronUp, Copy, GraduationCap, X } from "lucide-react";
import { useT } from "../i18n.tsx";
import { useDialogs } from "../ui/dialogs.tsx";
import { href } from "../router.ts";
import { Button, ICON } from "./dz/ui.tsx";
import { isNarrow } from "./device.ts";
import { TUTORIAL_STEPS, tutorialProgress, type TutorialFacts, type TutorialState, type TutorialStep } from "./tutorial.ts";

type I18n = ReturnType<typeof useT>;

interface StepText {
  title: string;
  body: string;
  /** Text to paste into the step, in the author's language. */
  example?: string;
  /** What the "show me" button says; it does what the act of the same step does. */
  action?: string;
  /** Other words on a phone, where steps open in their simple form and nothing is dragged. */
  phone?: Partial<Omit<StepText, "phone">>;
}

/** What each step asks, in words. Labels are the designer's own, so they can be found on screen. */
function texts(t: I18n): Record<TutorialStep, StepText> {
  return {
    welcome: {
      title: t.t("Write the welcome"),
      body: t.t("Click the first step after Start: it carries the adventure's title. Under What the team sees, write what the teams read first: who needs help, and why."),
      example: t.t("Hi! I'm Erik, the zookeeper. Last night someone opened the gate and my three elephants **wandered off** into the park.\n\nFollow the clues, and hurry: they get grumpy after dark!"),
      action: t.t("Open the welcome"),
      phone: {
        body: t.t("Tap the first step after Start: it carries the adventure's title. Under What the team reads, write what the teams read first: who needs help, and why."),
      },
    },
    zone: {
      title: t.t("Mark a place on the map"),
      body: t.t("In Adventure settings, under Zones, press Add zone. Call it The big oak and drag the pin on its map onto a tree near you. Keep the radius at 30 metres or more: GPS drifts between buildings. Then close the settings."),
      action: t.t("Open Adventure settings"),
      phone: {
        body: t.t("Add a step after the welcome, go to the place and tap Mark where I stand. Not there right now? Add the place in ⋮ → Adventure settings → Zones instead, and pick it under Where the team reads it."),
        action: t.t("Add a step after the welcome"),
      },
    },
    checkin: {
      title: t.t("Send the team there"),
      body: t.t("Add a step after the welcome: type check in in the search box and pick Check in at a place. Give it a name at the top, write where to go, and under Rules pick The big oak. The team can only continue once it stands there."),
      example: t.t("Elephant footprints! They lead to the biggest tree in the park. Go there."),
      action: t.t("Add a step after the welcome"),
      phone: {
        body: t.t("In the new step, give it a name and write where to go under What the team reads. Check that your place is chosen under Where the team reads it: the team can only continue once it stands there."),
        action: t.t("Open the check-in"),
      },
    },
    puzzle: {
      title: t.t("Ask a question"),
      body: t.t("Add a Puzzle after the check-in and name it, say Count the trunks: players see the name as a heading. Write the question, then under Answers and next steps type the answer, 7. Press Also accept to take seven too. Capital letters and extra spaces never matter."),
      example: t.t("Erik left a note in the bark: “Count the elephant trunks painted on the sign by the pond.”\n\nHow many trunks are there?"),
      action: t.t("Add a step after the check-in"),
      phone: {
        body: t.t("Add a puzzle after the check-in and name it, say Count the trunks: players see the name as a heading. Write the question under What the team reads, and the answer, 7, under Correct answer. Tap + Also accept another answer to take seven too."),
      },
    },
    hint: {
      title: t.t("Help a stuck team"),
      body: t.t("In the puzzle, open Hints and press Add hint. It unlocks after the time you set, counted from when the team arrived, so a fast team never sees it."),
      example: t.t("The sign is on the side of the pond that faces the playground."),
      action: t.t("Open the puzzle"),
      phone: {
        body: t.t("Open the puzzle, tap More options, then Hints, and press Add hint. It unlocks after the time you set, counted from when the team arrived, so a fast team never sees it."),
      },
    },
    choice: {
      title: t.t("Let the team choose a way"),
      body: t.t("Add a Choice after the puzzle and write the question. Under Answers and next steps, type left and right as its two answers: left already leads on to the end. For right, select the choice, press Add step and pick Story. Then select that story, press Add step, type the choice's name and pick it under Existing steps, so the team goes back to choose again."),
      example: t.t("Two paths leave the pond. On the left the grass is flattened, and someone dropped half a banana. On the right the path is neat and tidy.\n\nLeft or right?"),
      action: t.t("Add a step after the puzzle"),
      phone: {
        body: t.t("Add a choice after the puzzle. It comes ready with the answers left and right: left goes on to the end, and right to a step that sends the team back. Write the question under What the team reads; right is under More options, in Answers and next steps."),
        action: t.t("Add the choice"),
      },
    },
    finish: {
      title: t.t("Tie up the loose ends"),
      body: t.t("Every way out of a step must lead somewhere, at least one must reach an end, and every page needs something to read. Fix what the red count in the toolbar lists, fill in any step that says No text yet, then press Tidy to line the cards up."),
      action: t.t("Show what to fix"),
      phone: {
        body: t.t("Every way out of a step must lead somewhere, at least one must reach an end, and every page needs something to read. Tap the red count at the top to see what to fix, and fill in any step that is still empty."),
      },
    },
    look: {
      title: t.t("Make it look the part"),
      body: t.t("In Adventure settings, under Theme, press Change theme… to pick another look, or Customize… to change its colours, background or cover picture. Players see the changes when you publish."),
      action: t.t("Open Adventure settings"),
    },
    test: {
      title: t.t("Play it yourself"),
      body: t.t("Press Test play. To pass the check-in, pick your place under Team is at. Press +5 min to see the hint unlock, and try a wrong answer too."),
      action: t.t("Start Test play"),
      phone: {
        body: t.t("Tap Test play, the ▶ at the top. To pass the check-in, pick your place under Team is at, or go there and test with your own location. Tap +5 min to see the hint unlock."),
      },
    },
    publish: {
      title: t.t("Publish it"),
      body: t.t("Press Publish. Events that start from now on play this version; what you change afterwards stays in the draft until you publish again."),
      action: t.t("Publish"),
    },
  };
}

/**
 * The tutorial's guide, docked over the canvas: the steps of Erik's
 * stolen elephants, ticked off as the draft gets there, with the current
 * one open — what to do, text to paste, and a button that takes the
 * author to the right place.
 */
export function Tutorial({
  slug,
  state,
  facts,
  act,
  onChange,
}: {
  slug: string;
  state: TutorialState;
  facts: TutorialFacts;
  act: Partial<Record<TutorialStep, () => void>>;
  onChange: (s: TutorialState | null) => void;
}) {
  const t = useT();
  // The step at hand shows alone, the others a tap away, so the guide
  // covers little of the canvas. On a phone it is a bar along the bottom,
  // steps open in their simple form, and the words follow suit.
  const narrow = isNarrow();
  const words = Object.fromEntries(Object.entries(texts(t)).map(([k, w]) => [k, narrow ? { ...w, ...w.phone } : w])) as Record<TutorialStep, StepText>;
  const [all, setAll] = useState(false);
  const done = tutorialProgress(state, facts);
  const count = TUTORIAL_STEPS.filter((x) => done[x]).length;
  const current = TUTORIAL_STEPS.find((x) => !done[x]) ?? null;
  const [picked, setPicked] = useState<TutorialStep | null>(null);
  const open = picked ?? current;
  const [copied, setCopied] = useState(false);

  const tick = (x: TutorialStep, on: boolean) => {
    onChange({ ...state, done: on ? [...new Set([...state.done, x])] : state.done.filter((d) => d !== x) });
    setPicked(null);
  };
  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };
  // Done a step's thing on a phone, the guide gets out of the way of the form it opened.
  const go = (x: TutorialStep) => {
    act[x]?.();
    if (narrow) onChange({ ...state, collapsed: true });
  };
  const dialogs = useDialogs();
  const stop = async () => {
    if (
      count === TUTORIAL_STEPS.length ||
      (await dialogs.confirm({
        title: t.t("Stop this tutorial?"),
        message: t.t("Your adventure stays as it is. You can start it again from the ⋮ menu."),
        confirmLabel: t.t("Stop"),
        cancelLabel: t.t("Keep going"),
      }))
    )
      onChange(null);
  };

  if (state.collapsed)
    return (
      <button type="button" className="tut-pill" onClick={() => onChange({ ...state, collapsed: false })} title={t.t("Show the tutorial")} aria-label={t.t("Show the tutorial")}>
        <GraduationCap size={ICON.sm} aria-hidden />
        <span>
          {count}/{TUTORIAL_STEPS.length}
          {current && <span className="tut-pill__step"> · {words[current].title}</span>}
        </span>
      </button>
    );

  const step = open && words[open];
  return (
    <aside className="tut" aria-label={t.t("Tutorial")}>
      <header className="tut__head">
        <GraduationCap size={ICON.md} aria-hidden />
        <strong>{t.t("Tutorial: your first adventure")}</strong>
        <Button variant="ghost" size="icon" onClick={() => onChange({ ...state, collapsed: true })} aria-label={t.t("Hide the tutorial")} title={t.t("Hide the tutorial")}>
          <ChevronDown size={ICON.md} />
        </Button>
        <Button variant="ghost" size="icon" onClick={() => void stop()} aria-label={t.t("Stop the tutorial")} title={t.t("Stop the tutorial")}>
          <X size={ICON.md} />
        </Button>
      </header>
      <div className="tut__bar" role="progressbar" aria-valuemin={0} aria-valuemax={TUTORIAL_STEPS.length} aria-valuenow={count}>
        <span style={{ width: `${(count / TUTORIAL_STEPS.length) * 100}%` }} />
      </div>
      <ol className="tut__steps">
        {TUTORIAL_STEPS.map((x, i) => (!all && x !== open ? null : (
          <li key={x} className={(done[x] ? "is-done" : "") + (x === open ? " is-open" : "")}>
            <button type="button" className="tut__step" aria-expanded={x === open} onClick={() => setPicked(x === open ? null : x)}>
              <span className="tut__n" aria-hidden>{done[x] ? <Check size={ICON.xs} /> : i + 1}</span>
              <span className="tut__title">{words[x].title}</span>
              {x === open ? <ChevronUp size={ICON.sm} aria-hidden /> : null}
            </button>
            {x === open && step && (
              <div className="tut__body">
                <p>{step.body}</p>
                {step.example && (
                  <div className="tut__example">
                    <pre>{step.example}</pre>
                    <Button variant="ghost" size="sm" onClick={() => copy(step.example!)}>
                      {copied ? <Check size={ICON.xs} aria-hidden /> : <Copy size={ICON.xs} aria-hidden />}
                      {copied ? t.t("Copied") : t.t("Copy")}
                    </Button>
                  </div>
                )}
                <div className="tut__actions">
                  {step.action && act[x] && (
                    <Button variant="primary" size="sm" onClick={() => go(x)}>
                      {step.action}
                    </Button>
                  )}
                  {done[x] ? (
                    state.done.includes(x) && (
                      <Button variant="ghost" size="sm" onClick={() => tick(x, false)}>
                        {t.t("Not done after all")}
                      </Button>
                    )
                  ) : (
                    <Button variant="ghost" size="sm" onClick={() => tick(x, true)}>
                      {t.t("Mark as done")}
                    </Button>
                  )}
                </div>
              </div>
            )}
          </li>
        )))}
      </ol>
      {current && (
        <button type="button" className="tut__all" onClick={() => setAll(!all)}>
          {all ? t.t("Only the step at hand") : t.t("All steps ({{done}} of {{n}} done)", { done: count, n: TUTORIAL_STEPS.length })}
        </button>
      )}
      {!current && (
        <div className="tut__done">
          <p>
            <strong>{t.t("You built an adventure!")}</strong> {t.t("Schedule an event from the adventure's page to play it for real, with teams and their own phones.")}
          </p>
          <div className="tut__actions">
            <Button variant="primary" size="sm" onClick={() => (location.hash = href({ name: "adventure", slug }))}>
              {t.t("Go to the adventure")}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => onChange(null)}>
              {t.t("Close the tutorial")}
            </Button>
          </div>
        </div>
      )}
    </aside>
  );
}
