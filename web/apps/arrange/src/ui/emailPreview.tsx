import { useState } from "react";
import { Check, Monitor, Send, Smartphone } from "lucide-react";
import type { emails } from "@zonventure/api";
import { useT } from "../i18n.tsx";
import "./emailPreview.css";

/**
 * An email as its reader will see it: the subject as an inbox shows it,
 * and the message itself, at a desktop's width or a phone's. The email
 * renders in a sandboxed frame — no scripts, its own styles — as a mail
 * client would show it.
 */
export function EmailPreview({
  preview,
  loading,
  from,
  onSendTest,
}: {
  preview: emails.Preview | null;
  loading?: boolean;
  from: string;
  /** Send what is shown to the viewer's own address; the button shows only with it. */
  onSendTest?: () => Promise<{ to: string }>;
}) {
  const t = useT();
  const [narrow, setNarrow] = useState(false);
  const [test, setTest] = useState<{ state: "idle" | "sending" | "sent" | "failed"; note?: string }>({ state: "idle" });
  const sendTest = async () => {
    if (!onSendTest) return;
    setTest({ state: "sending" });
    try {
      const r = await onSendTest();
      setTest({ state: "sent", note: t.t("Sent to {{to}}", { to: r.to }) });
      setTimeout(() => setTest((x) => (x.state === "sent" ? { state: "idle" } : x)), 4000);
    } catch (e) {
      setTest({ state: "failed", note: e instanceof Error ? e.message : String(e) });
    }
  };
  return (
    <div className={"mailprev" + (loading ? " is-loading" : "")}>
      <div className="mailprev__bar">
        <div className="mailprev__meta">
          <span className="mailprev__from">{from}</span>
          <span className="mailprev__subject">{preview?.subject ?? "…"}</span>
        </div>
        {onSendTest && (
          <span className="mailprev__test">
            {test.note && (
              <span className={"mailprev__note" + (test.state === "failed" ? " is-err" : "")} role="status">
                {test.state === "sent" && <Check size={13} aria-hidden />}
                {test.note}
              </span>
            )}
            <button type="button" className="mailprev__send" onClick={() => void sendTest()} disabled={test.state === "sending" || !preview} title={t.t("Send this email to your own address, to see it in your inbox")}>
              <Send size={13} aria-hidden />
              {test.state === "sending" ? t.t("Sending…") : t.t("Send me a test")}
            </button>
          </span>
        )}
        <div className="mailprev__sizes" role="group" aria-label={t.t("Preview width")}>
          <button type="button" className={!narrow ? "is-on" : ""} aria-pressed={!narrow} onClick={() => setNarrow(false)} title={t.t("As on a computer")} aria-label={t.t("As on a computer")}>
            <Monitor size={14} />
          </button>
          <button type="button" className={narrow ? "is-on" : ""} aria-pressed={narrow} onClick={() => setNarrow(true)} title={t.t("As on a phone")} aria-label={t.t("As on a phone")}>
            <Smartphone size={14} />
          </button>
        </div>
      </div>
      <div className="mailprev__stage">
        {preview && (
          <iframe
            className={"mailprev__frame" + (narrow ? " is-narrow" : "")}
            title={t.t("Email preview")}
            sandbox="allow-popups"
            srcDoc={preview.html}
          />
        )}
      </div>
    </div>
  );
}
