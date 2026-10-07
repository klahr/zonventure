import { useState } from "react";
import { flagFor } from "@zonventure/i18n";
import { coverOf } from "@zonventure/play-ui";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, href } from "../../router.ts";
import { Pill, Tabs, initials } from "../../ui/index.ts";

/**
 * An adventure's cover: the uploaded picture, whole (a logo is not
 * cropped), or its initials on the accent colour. Rounded square — the
 * shape reserved for adventures; teams are circles.
 */
export function Cover({ title, src, photo, size = 48, accent }: { title: string; src?: string | undefined; photo?: string | undefined; size?: number; accent?: string | undefined }) {
  const [broken, setBroken] = useState<string[]>([]);
  const style = { width: size, height: size, borderRadius: Math.max(6, Math.round(size * 0.18)), fontSize: Math.max(11, Math.round(size * 0.34)) };
  const pic = [src, photo].find((u): u is string => !!u && !broken.includes(u));
  if (pic) {
    return (
      <span className={"adv-cover adv-cover--img" + (pic === photo ? " adv-cover--photo" : "")} style={style}>
        <img src={pic} alt="" onError={() => setBroken((b) => [...b, pic])} />
      </span>
    );
  }
  return (
    <span className="adv-cover" style={{ ...style, background: accent || "var(--dz-accent)" }} aria-hidden>
      {initials(title)}
    </span>
  );
}

/**
 * The look's cover as the players see it — its own picture, else its
 * starter theme's — for an adventure with no icon of its own.
 */
export function lookCover(theme: string | undefined, cover: number | undefined): string | undefined {
  return coverOf(theme, cover ? `/media/${cover}` : undefined)?.small;
}

/** An adventure's icon, else its look's cover. */
export function iconOrCover(a: { avatar_url?: string | undefined; theme?: string | undefined; cover?: number | undefined }): string | undefined {
  return a.avatar_url ?? lookCover(a.theme, a.cover);
}

/** Yours / Catalog, under the Adventures title. */
export function AdventureTabs({ current }: { current: "yours" | "catalog" }) {
  const t = useT();
  if (!CATALOG_ENABLED) return null;
  return (
    <Tabs
      label={t.t("Adventures")}
      current={current}
      tabs={[
        { id: "yours", label: t.t("Yours"), href: href({ name: "adventures" }) },
        { id: "catalog", label: t.t("Catalog"), href: href({ name: "catalog" }) },
      ]}
    />
  );
}

export function LanguagePill({ code }: { code?: string | undefined }) {
  const t = useT();
  if (!code) return null;
  const flag = flagFor(code);
  return (
    <Pill title={t.t("Adventure content language")}>
      {flag && <span aria-hidden>{flag}</span>}
      <span className="adv-lang">{code}</span>
    </Pill>
  );
}

export function puzzleCount(t: ReturnType<typeof useT>, n: number): string {
  return t.plural(n, { one: t.t("{{n}} puzzle"), other: t.t("{{n}} puzzles") });
}
