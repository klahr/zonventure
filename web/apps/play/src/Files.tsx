import { play } from "@zonventure/api";
import type { Translate } from "@zonventure/play-ui";

/** A file's size, in the reader's words: "340 kB", "2.4 MB". */
export function fmtSize(bytes: number, locale?: string): string {
  const mb = bytes >= 1_000_000;
  return new Intl.NumberFormat(locale, {
    style: "unit",
    unit: mb ? "megabyte" : "kilobyte",
    maximumFractionDigits: mb && bytes < 10_000_000 ? 1 : 0,
  }).format(Math.max(1, mb ? bytes / 1_000_000 : bytes / 1000));
}

/** What the organisers left for the players to download, one link each. */
export function Files({ files, t, locale }: { files: play.EventFile[]; t: Translate; locale?: string }) {
  if (files.length === 0) return null;
  return (
    <section className="app-files" aria-label={t("Files from the organisers")}>
      <h2 className="app-files__title">{t("Files from the organisers")}</h2>
      <ul>
        {files.map((f) => (
          <li key={f.id}>
            <a href={play.fileURL(f.id)} download={f.name}>
              <span className="app-files__name">{f.name}</span>
              <span className="app-files__size">{fmtSize(f.size, locale)}</span>
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}
