import { useRef, useState } from "react";
import { Download, Paperclip, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Card, fmtDateTime, fmtWhen, ICON, useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { EventFile, EventInfo } from "./api.ts";
import { languageName } from "../../languages.ts";

type Info = Pick<EventInfo, "id" | "state" | "languages"> & { public_for_sale?: boolean | undefined };

/** What the server takes; it checks the bytes, this only steers the picker. */
const ACCEPT = ".pdf,.png,.jpg,.jpeg,.gif,.webp,.txt,.docx,.xlsx,.pptx,.odt,.ods,.odp";

/** A file's size, in the reader's words: "340 kB", "2.4 MB". */
function fmtSize(bytes: number, locale?: string): string {
  const mb = bytes >= 1_000_000;
  return new Intl.NumberFormat(locale, {
    style: "unit",
    unit: mb ? "megabyte" : "kilobyte",
    maximumFractionDigits: mb && bytes < 10_000_000 ? 1 : 0,
  }).format(Math.max(1, mb ? bytes / 1_000_000 : bytes / 1000));
}

/**
 * Files the players can download from the app before the start, and all
 * through the game: rules, a map to print, a packing list. An ended
 * event that has none has no card.
 */
export function FilesCard({ event, files, maxFiles, maxSize, reload }: { event: Info; files: EventFile[]; maxFiles: number; maxSize: number; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);
  const canAdd = event.state !== "completed" && event.state !== "cancelled";
  if (files.length === 0 && !canAdd) return null;
  const full = files.length >= maxFiles;

  const upload = async (picked: File[]) => {
    const room = maxFiles - files.length;
    if (picked.length > room) toast.err(t.t("An event can have at most {{n}} files.", { n: maxFiles }));
    setBusy(true);
    const ids: number[] = [];
    for (const f of picked.slice(0, room)) {
      if (f.size > maxSize) {
        toast.err(t.t("{{name}} is too large. A file can be at most {{mb}} MB.", { name: f.name, mb: Math.floor(maxSize / (1 << 20)) }));
        continue;
      }
      try {
        ids.push((await api.uploadFile(event.id, f)).id);
      } catch (e) {
        toast.err(e);
      }
    }
    // One message to the teams for the whole batch, not one per file.
    if (ids.length > 0) await api.announceFiles(event.id, ids).catch((e: unknown) => toast.err(e));
    const added = ids.length;
    setBusy(false);
    if (added > 0) {
      // On an event sold in the store a new file waits for review with the listing.
      if (event.public_for_sale) toast.ok(t.t("File added. Players get it once the listing is approved: submit the listing for review."));
      else toast.ok(added === 1 ? t.t("File added. Players can download it now.") : t.t("{{n}} files added. Players can download them now.", { n: added }));
      reload();
    }
  };
  // A file in one language goes to the teams playing in it; a team whose
  // language has none gets those in the adventure's own.
  const languages = event.languages ?? [];
  const setLang = async (f: EventFile, lang: string) => {
    try {
      await api.setFileLang(event.id, f.id, lang);
      reload();
    } catch (e) {
      toast.err(e);
    }
  };
  const remove = async (f: EventFile) => {
    const ok = await dialogs.confirm({
      title: t.t("Delete this file?"),
      message: <>“{f.name}” {t.t("Players can no longer download it. Copies they already saved stay theirs.")}</>,
      confirmLabel: t.t("Delete"),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteFile(event.id, f.id);
      toast.ok(t.t("File deleted."));
      reload();
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <Card
      title={t.t("Files for players")}
      actions={
        canAdd && (
          <Button size="sm" loading={busy} disabled={full} title={full ? t.t("An event can have at most {{n}} files.", { n: maxFiles }) : undefined} icon={<Paperclip size={ICON.sm} aria-hidden />} onClick={() => input.current?.click()}>
            {t.t("Add files")}
          </Button>
        )
      }
    >
      <input
        ref={input}
        type="file"
        accept={ACCEPT}
        multiple
        hidden
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? []);
          e.target.value = "";
          if (picked.length) void upload(picked);
        }}
      />
      {files.length === 0 ? (
        <p className="cs-muted">{t.t("Nothing yet. Add a PDF with the rules, a map to print or a packing list: players download it from the app before the start, and keep it at hand in the menu during the game.")}</p>
      ) : (
        <ul className="ev-msgs ev-files">
          {files.map((f) => (
            <li key={f.id}>
              <a className="ev-files__name" href={api.fileURL(event.id, f.id)} download={f.name}>
                <Download size={ICON.sm} aria-hidden /> {f.name}
              </a>
              <Button variant="ghost" size="icon" aria-label={t.t("Delete file")} title={t.t("Delete file")} icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={() => void remove(f)} />
              <div className="ev-msgs__meta cs-muted">
                {fmtSize(f.size, t.current())} · <time dateTime={f.created_at} title={fmtDateTime(f.created_at, t.current())}>{fmtWhen(f.created_at, t.current())}</time>
                {languages.length > 1 && (
                  <>
                    {" · "}
                    <select aria-label={t.t("Who gets {{name}}", { name: f.name })} value={f.lang ?? ""} onChange={(e) => void setLang(f, e.target.value)}>
                      <option value="">{t.t("Every team")}</option>
                      {languages.map((l) => <option key={l} value={l}>{t.t("Teams playing in {{lang}}", { lang: languageName(l) })}</option>)}
                    </select>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {canAdd && <p className="cs-sub">{t.t("PDF, pictures, text or Office documents: up to {{n}} files, {{mb}} MB each.", { n: maxFiles, mb: Math.floor(maxSize / (1 << 20)) })}</p>}
      {event.public_for_sale && (
        <p className="cs-sub">{t.t("This event is sold in the store, so a new file reaches players once the listing is approved with it. Deleting a file takes it away at once.")}</p>
      )}
    </Card>
  );
}
