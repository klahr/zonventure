import type { Block, Graph, Node } from "@zonventure/graph";
import type { T } from "./types.ts";

// A text the author left empty comes back from the server without it.
const clip = (s: string | undefined, n = 48) => {
  const one = (s ?? "").replace(/[#*_>`[\]]/g, "").replace(/\s+/g, " ").trim();
  return one.length > n ? one.slice(0, n - 1) + "…" : one;
};

/** One line saying what a collapsed block holds, so a folded list still reads. */
export function summarize(b: Block, graph: Graph, t: T, node: Node): string {
  const zone = (slug: string) => {
    const z = graph.zones?.find((x) => x.slug === slug);
    return z ? z.name || z.slug : t.t("no zone yet");
  };
  switch (b.kind) {
    case "text":
    case "note":
      return clip(b.text) || t.t("empty");
    case "image":
    case "audio":
    case "video":
    case "hotspots":
    case "overlay":
    case "torch":
    case "spotdiff":
      return clip(b.alt ?? "") || t.t("no description");
    case "call":
      return b.who || clip(b.alt ?? "") || t.t("no caller");
    case "split":
      return t.t("{{n}} parts", { n: b.parts.length });
    case "bearing":
      return t.t("{{n}}°", { n: b.heading ?? 0 });
    case "shake":
      return t.t("shake {{n}}", { n: b.threshold ?? 60 });
    case "suspects":
      return b.cards.map((c) => c.name).filter(Boolean).join(", ") || t.t("no suspects yet");
    case "photo":
      return (clip(b.text) || t.t("no prompt yet")) + (b.worth ? ` · ${t.t("{{n}} points", { n: b.worth })}` : "");
    case "flasher":
    case "letters":
      return clip(b.text) || t.t("empty");
    case "vote":
      return clip(b.text) || t.t("no question yet");
    case "sabotage":
      return [b.worth ? t.t("−{{n}} points", { n: b.worth }) : "", b.seconds ? t.t("locks {{n}} s", { n: b.seconds }) : "", b.journal ? t.t("a false note") : ""].filter(Boolean).join(" · ") || t.t("does nothing yet");
    case "accuse":
      return clip(b.text) || t.t("no question yet");
    case "mission":
      return t.t("{{n}} players pick", { n: Math.max(b.count ?? 1, 1) }) + (b.zone ? ` · ${b.zone}` : ` · ${t.t("no post yet")}`);
    case "roles":
      return [b.team ? t.t("everyone's") : t.t("the player's own"), b.choose ? t.t("chosen here") : "", b.lock ? t.t("final after this step") : ""].filter(Boolean).join(" · ");
    case "draw":
      return t.t("{{n}} cards", { n: b.options.length });
    case "bulletin":
      return "";
    case "compass":
    case "map":
    case "radar":
      return zone(b.zone);
    case "chat":
      return t.t("{{n}} messages", { n: b.lines.length });
    case "cipher":
      return b.cipher === "caesar" ? t.t(b.wheel ? "Caesar wheel, shift {{n}}" : "Caesar, shift {{n}}", { n: b.shift ?? 3 }) : b.cipher;
    case "divider":
      return b.style ?? "line";
    case "level":
      return b.pitch ? t.t("{{n}}° up", { n: b.pitch }) : t.t("flat");
    case "noise":
      return t.t("loudness {{n}}", { n: b.threshold ?? 70 });
    case "countdown":
      return t.t("{{n}} min", { n: Math.round(b.seconds / 60) });
    case "status":
      return [b.items ? t.t("items") : "", ...(b.counters ?? [])].filter(Boolean).join(", ");
    case "standings":
      return "";
    case "quiz":
      return (clip(b.text) || t.t("no question yet")) + (b.answer ? ` · ${b.answer}` : "");
    case "tiebreak":
      return (clip(b.text) || t.t("no question yet")) + (b.target !== undefined ? ` · ${b.target}` : "");
    case "title":
      return node.title ?? "";
    case "points": {
      const best = Math.max(0, ...(node.puzzle?.outputs ?? []).filter((o) => o.trigger === "answer").map((o) => o.points ?? 0));
      return best ? t.t("{{n}} points", { n: best }) : t.t("none");
    }
  }
}
