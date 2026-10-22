/**
 * Display helpers for a listing's texts in "Review in full". The server
 * names each text by its JSON path in the adventure ("Adventure:
 * nodes[2].story.blocks[1].text"); those paths are fingerprinted in the
 * review log, so they stay as they are and are put in words here.
 */

type T = { t: (key: string, vars?: Record<string, string | number>) => string };

export interface Text {
  where: string;
  text: string;
}

const GRAPH = "Adventure: ";

/** Settings the adventure stores as words, which players never read. */
const NOT_PLAYER = /^(language|.*\.(outcome|delivery|policy|op))$/;

/** Whether a text is one players or buyers read. */
export function isPlayerText(where: string): boolean {
  return !(where.startsWith(GRAPH) && NOT_PLAYER.test(where.slice(GRAPH.length)));
}

const seg = (s: string) => {
  const m = /^([^[]+)(?:\[(\d+)\])?$/.exec(s);
  return m ? { name: m[1] ?? s, i: m[2] === undefined ? -1 : Number(m[2]) } : { name: s, i: -1 };
};

/** Where a text is, in words: "Step 3 · Bridge · puzzle answer". Anything unrecognised stays as the server wrote it. */
export function textLabel(where: string, stepTitles: readonly string[] | undefined, t: T): string {
  if (!where.startsWith(GRAPH)) return where;
  const path = where.slice(GRAPH.length).split(".");
  const head = seg(path[0] ?? "");
  if (head.i < 0) {
    if (head.name === "translations" && path.length >= 3) return t.t("Translation ({{lang}}) · {{key}}", { lang: path[1] ?? "", key: path.slice(2).join(".") });
    return where;
  }
  const n = head.i + 1;
  let place: string;
  switch (head.name) {
    case "nodes":
      place = stepTitles?.[head.i] ? t.t("Step {{n}} · {{title}}", { n, title: stepTitles[head.i] ?? "" }) : t.t("Step {{n}}", { n });
      break;
    case "zones":
      place = t.t("Zone {{n}}", { n });
      break;
    case "items":
      place = t.t("Item {{n}}", { n });
      break;
    case "roles":
      place = t.t("Role {{n}}", { n });
      break;
    case "journal":
      place = t.t("Journal entry {{n}}", { n });
      break;
    default:
      return where;
  }
  const words: Record<string, string> = {
    story: t.t("story"),
    puzzle: t.t("puzzle"),
    end: t.t("ending"),
    hub: t.t("crossroads"),
    gate: t.t("gate"),
    start: t.t("start"),
    effect: t.t("effect"),
    switch: t.t("switch"),
    split: t.t("split"),
    paths: t.t("group"),
    title: t.t("title"),
    text: t.t("text"),
    label: t.t("label"),
    hints: t.t("hint"),
    responses: t.t("response"),
    matchers: t.t("answer"),
    outputs: t.t("choice"),
    options: t.t("option"),
    alt: t.t("picture description"),
    caption: t.t("caption"),
    prompt: t.t("question"),
  };
  const kinds = new Set(["story", "puzzle", "end", "hub", "gate", "start", "effect", "switch", "split"]);
  const field: string[] = [];
  const rest = path.slice(1).map((s) => seg(s).name);
  let named = false;
  for (let k = 0; k < rest.length; k++) {
    const name = rest[k] ?? "";
    if (name === "blocks" || name === "media") continue;
    if (name === "outputs" && rest[k + 1] === "matchers") continue;
    // "text" and "value" only repeat what a hint, answer or choice already says.
    if ((name === "text" || name === "value") && named) continue;
    field.push(words[name] ?? name);
    if (!kinds.has(name)) named = true;
    if (name === "matchers") break;
  }
  return field.length ? `${place} · ${field.join(" ")}` : place;
}

/** What changed between the copy a reviewer looked at and the one now: each place whose text differs, with what it was. */
export function changedTexts(before: readonly Text[], after: readonly Text[]): { where: string; text: string; was: string }[] {
  const join = (xs: readonly Text[]) => {
    const m = new Map<string, string>();
    for (const x of xs) m.set(x.where, m.has(x.where) ? `${m.get(x.where)}\n${x.text}` : x.text);
    return m;
  };
  const a = join(before);
  const b = join(after);
  const out: { where: string; text: string; was: string }[] = [];
  for (const [where, text] of b) if (a.get(where) !== text) out.push({ where, text, was: a.get(where) ?? "" });
  for (const [where, was] of a) if (!b.has(where)) out.push({ where, text: "", was });
  return out;
}
