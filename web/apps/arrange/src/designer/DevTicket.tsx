import { useEffect, useState } from "react";
import { adventures } from "@zonventure/api";
import { useT } from "../i18n.tsx";
import { PanelModal } from "./dz/dialogs.tsx";
import { Button } from "./dz/ui.tsx";

/**
 * The author's dev ticket: a phone link that plays the saved draft live —
 * every save, here or by an assistant through the MCP, shows on the phone
 * within a couple of seconds, with nothing published. Scan it, open it, or copy the link.
 */
export function DevTicketModal({ slug, onClose }: { slug: string; onClose: () => void }) {
  const t = useT();
  const [ticket, setTicket] = useState<adventures.DevTicket | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [sending, setSending] = useState(false);
  useEffect(() => {
    adventures
      .createDevTicket(slug)
      .then(setTicket)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : t.t("Could not make the dev ticket.")));
  }, [slug, t]);
  const token = ticket?.team_url.split("/team/")[1];
  return (
    <PanelModal title={t.t("Play the draft on my phone")} onClose={onClose}>
      <div className="devticket">
        {error && <div className="alert alert--danger">{error}</div>}
        {!ticket && !error && <p className="field__hint">{t.t("Making your dev ticket…")}</p>}
        {ticket && (
          <>
            <p className="field__hint">{t.t("Scan with your phone. It plays the saved draft: every change — yours here, or an assistant's — shows on the phone within a couple of seconds. Nothing is published, and it never counts as an event.")}</p>
            {token && <img className="devticket__qr" src={`/qr/team/${encodeURIComponent(token)}`} alt={t.t("QR code for the dev ticket")} />}
            <div className="look__actions">
              <input className="field__input" readOnly value={ticket.team_url} onFocus={(e) => e.target.select()} aria-label={t.t("Link")} />
              <Button
                type="button"
                size="sm"
                onClick={() => void navigator.clipboard?.writeText(ticket.team_url).then(() => setNote(t.t("Link copied")))}
             >
                {t.t("Copy link")}
              </Button>
              <a className="btn sm" href={ticket.team_url} target="_blank" rel="noopener noreferrer">
                {t.t("Open")}
              </a>
            </div>
            {ticket.voucher_code && <p className="field__hint">{t.t("Or open /play on the phone and enter {{code}} as the voucher code.", { code: ticket.voucher_code })}</p>}
            <form
              className="look__actions"
              onSubmit={(e) => {
                e.preventDefault();
                if (!email.trim() || sending) return;
                setSending(true);
                adventures
                  .sendDevTicket(slug, email.trim())
                  .then(() => {
                    setNote(t.t("Sent to {{email}}", { email: email.trim() }));
                    setEmail("");
                  })
                  .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
                  .finally(() => setSending(false));
              }}
            >
              <input
                className="field__input"
                type="email"
                value={email}
                placeholder="name@example.com"
                aria-label={t.t("Send to an email address")}
                onChange={(e) => setEmail(e.target.value)}
              />
              <Button type="submit" size="sm" disabled={!email.trim() || sending}>
                {sending ? t.t("Sending…") : t.t("Send")}
              </Button>
            </form>
            <div className="look__actions">
              <Button
                type="button"
                size="sm"
                onClick={() =>
                  void adventures
                    .devReset(slug)
                    .then(() => setNote(t.t("The phone starts over.")))
                    .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
                }
             >
                {t.t("Start over")}
              </Button>
              {note && <span className="field__hint">{note}</span>}
            </div>
            <p className="field__hint">{t.t("On the phone, the DEV bar goes to any step — back or on — or starts over.")}</p>
          </>
        )}
      </div>
    </PanelModal>
  );
}
