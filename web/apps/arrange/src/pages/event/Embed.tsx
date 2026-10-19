import { useState } from "react";
import { Copy } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Card, Field, ICON, Row, useToast } from "../../ui/index.ts";
import type { EventInfo } from "./api.ts";
import { copyText } from "./util.ts";

/** The embed's two sizes, as internal/server/embed-event.html lays them out. */
const SIZES = {
  button: { width: 360, height: 76 },
  card: { width: 340, height: 430 },
} as const;
type Size = keyof typeof SIZES;

/**
 * A self-guided event's buy button or card, for the organisation's own
 * site or anyone selling it: the code to paste, a preview of it, and a
 * plain link for places that don't take frames (newsletters, mail).
 */
export function EmbedCard({ event }: { event: EventInfo }) {
  const t = useT();
  const toast = useToast();
  const [size, setSize] = useState<Size>("card");
  const [lang, setLang] = useState(t.current() === "sv" ? "sv" : "en");
  const base = event.embed_base || location.origin;
  const path = `/embed/event/${event.id}?size=${size}&lang=${lang}`;
  const src = base + path;
  const { width, height } = SIZES[size];
  const title = event.name || event.adventure_title || "";
  const iframe = `<iframe src="${src}" width="${width}" height="${height}" style="border:0;max-width:100%" loading="lazy" title="${escapeAttr(title)}"></iframe>`;
  const link = `<a href="${base}/store/event/${event.id}?ref=embed" target="_blank" rel="noopener">${escapeText(title)}</a>`;
  const copy = async (text: string) => {
    if (await copyText(text)) toast.ok(t.t("Code copied"));
    else toast.err(t.t("Couldn't copy: select the code and copy it by hand."));
  };
  return (
    <Card title={t.t("Share & embed")} subtitle={t.t("Put a buy button for this adventure on any web page.")}>
      <Row>
        <Field label={t.t("Size")}>
          <select value={size} onChange={(e) => setSize(e.target.value as Size)}>
            <option value="card">{t.t("Card — picture, description and button")}</option>
            <option value="button">{t.t("Button — one row, for a sidebar or under an article")}</option>
          </select>
        </Field>
        <Field label={t.t("Language")}>
          <select value={lang} onChange={(e) => setLang(e.target.value)}>
            <option value="sv">Svenska</option>
            <option value="en">English</option>
          </select>
        </Field>
      </Row>
      <div className="ev-embed-preview">
        {/* From the console's own address: its policy frames nothing else, and
            the embed is the same on the organisation's domain, links and all. */}
        <iframe key={path} src={location.origin + path} width={width} height={height} title={t.t("Preview")} />
      </div>
      <Field label={t.t("Code to paste into the page")} hint={t.t("It shows the price and whether it is on sale as it is now, in your organisation's colours. Buying opens your store in a new tab.")}>
        <textarea readOnly rows={3} value={iframe} onFocus={(e) => e.currentTarget.select()} />
      </Field>
      <Row>
        <Button size="sm" variant="primary" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copy(iframe)}>{t.t("Copy code")}</Button>
      </Row>
      <Field label={t.t("Plain link")} hint={t.t("For newsletters, mail and sites that don't allow embedded frames.")}>
        <textarea readOnly rows={2} value={link} onFocus={(e) => e.currentTarget.select()} />
      </Field>
      <Row>
        <Button size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void copy(link)}>{t.t("Copy link")}</Button>
      </Row>
    </Card>
  );
}

function escapeAttr(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

function escapeText(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
