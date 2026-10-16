import { useState } from "react";
import type { HostedEvent } from "./api.ts";
import { useHosted } from "./context.tsx";

/**
 * The player link: a QR code to show or print, the link to copy, and a
 * way to mail it to someone. Teams open it, name themselves and play.
 */
export function PlayerLink({ event }: { event: HostedEvent }) {
  const { i18n, api, run, busy } = useHosted();
  const t = i18n.t;
  const [copied, setCopied] = useState(false);
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState<string | null>(null);
  const open = !event.expired && event.state !== "completed" && event.state !== "cancelled";

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(event.player_url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="hosted-link">
      <img className="hosted-link__qr" src={api.qr()} alt={t("QR code for the player link")} width={200} height={200} />
      <div className="hosted-link__body">
        <p className="zv-muted">{t("Each team opens this on one phone, gives itself a name and plays. The first team to join starts a game.")}</p>
        <div className="hosted-link__url">
          <code>{event.player_url}</code>
          <button type="button" className="zv-btn" onClick={() => void copy()}>
            {copied ? t("Copied") : t("Copy")}
          </button>
        </div>
        {open && (
          <form
            className="zv-inline"
            onSubmit={(e) => {
              e.preventDefault();
              const to = email.trim();
              void run(async () => {
                await api.sendPlayerLink(to);
                setSent(to);
                setEmail("");
              });
            }}
          >
            <label className="zv-field">
              <span>{t("Send the link by email")}</span>
              <input type="email" value={email} placeholder={t("name@example.com")} onChange={(e) => setEmail(e.target.value)} required />
            </label>
            <button className="zv-btn" disabled={busy || !email.trim()}>
              {t("Send")}
            </button>
          </form>
        )}
        {sent && (
          <p className="zv-muted" role="status">
            {t("Sent to {{email}}.", { email: sent })}
          </p>
        )}
      </div>
    </div>
  );
}
