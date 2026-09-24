import type { Block, Graph, Node } from "@zonventure/graph";
import type { T } from "./types.ts";

const clip = (s: string, n = 48) => {
  const one = s.replace(/[#*_>`[\]]/g, "").replace(/\s+/g, " ").trim();
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
      return clip(b.alt ?? "") || t.t("no description");
    case "compass":
    case "map":
    case "radar":
      return zone(b.zone);
    case "chat":
      return t.t("{{n}} messages", { n: b.lines.length });
    case "cipher":
      return b.cipher === "caesar" ? t.t("Caesar, shift {{n}}", { n: b.shift ?? 3 }) : b.cipher;
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
