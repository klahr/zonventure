import { useState } from "react";
import { flagFor } from "@zonventure/i18n";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, href } from "../../router.ts";
import { Pill, Tabs, initials } from "../../ui/index.ts";

/**
 * An adventure's cover: the uploaded picture, whole (a logo is not
 * cropped), or its initials on the accent colour. Rounded square — the
 * shape reserved for adventures; teams are circles.
 */
export function Cover({ title, src, size = 48, accent }: { title: string; src?: string | undefined; size?: number; accent?: string | undefined }) {
  const [broken, setBroken] = useState(false);
  const style = { width: size, height: size, borderRadius: Math.max(6, Math.round(size * 0.18)), fontSize: Math.max(11, Math.round(size * 0.34)) };
  if (src && !broken) {
    return (
      <span className="adv-cover adv-cover--img" style={style}>
        <img src={src} alt="" onError={() => setBroken(true)} />
      </span>
    );
  }
  return (
    <span className="adv-cover" style={{ ...style, background: accent || "var(--dz-accent)" }} aria-hidden>
      {initials(title)}
    </span>
  );
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
