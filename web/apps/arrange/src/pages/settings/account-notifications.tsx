import { useRef, useState } from "react";
import { emails } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Card, ErrorBox, Loading, errorText, useLoad } from "../../ui/index.ts";
import { Item } from "./account-security.tsx";

/** Which emails the user gets about the work they share with other designers. Each toggle saves at once. */
export function NotificationsCard() {
  const t = useT();
  const load = useLoad(() => emails.prefs(), []);
  const [err, setErr] = useState("");
  const kinds: { kind: emails.NotificationKind; label: string; hint: string }[] = [
    { kind: "mention", label: t.t("When someone mentions you"), hint: t.t("Someone @mentions you in a comment on an adventure.") },
    { kind: "step_assigned", label: t.t("When you're given a step"), hint: t.t("Someone makes you the owner of a step.") },
    { kind: "suggestion", label: t.t("Suggestions on your steps, and decisions on yours"), hint: t.t("Someone suggests a change to a step you own, or accepts or declines a suggestion you made.") },
    { kind: "review_requested", label: t.t("When your step is put up for review"), hint: t.t("Someone moves a step you own to review.") },
  ];

  const latest = useRef(load.data);
  latest.current = load.data;

  // Optimistic; a failed save puts back only its own toggle, so a quick
  // second change is not undone with it.
  const flip = (kind: emails.NotificationKind, on: boolean) => {
    const cur = latest.current;
    if (!cur) return;
    latest.current = { prefs: { ...cur.prefs, [kind]: on } };
    load.set(latest.current);
  };
  const toggle = async (kind: emails.NotificationKind, on: boolean) => {
    flip(kind, on);
    setErr("");
    try {
      await emails.setPrefs({ [kind]: on });
    } catch (e) {
      flip(kind, !on);
      setErr(errorText(e));
    }
  };

  let body;
  if (load.error && !load.data) body = <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  else if (!load.data) body = <Loading label={t.t("Loading…")} />;
  else {
    const prefs = load.data.prefs;
    body = (
      <>
        <div className="acct-list">
          {kinds.map((k) => (
            <Item
              key={k.kind}
              name={k.label}
              meta={k.hint}
              action={<input type="checkbox" role="switch" aria-label={k.label} checked={!!prefs[k.kind]} onChange={(e) => void toggle(k.kind, e.target.checked)} />}
            />
          ))}
        </div>
        {err && <p className="cs-err" role="alert">{err}</p>}
      </>
    );
  }
  return <Card title={t.t("Email notifications")}>{body}</Card>;
}
