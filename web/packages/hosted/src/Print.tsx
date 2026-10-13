import type { HostedEvent } from "./api.ts";
import { useHosted } from "./context.tsx";

/**
 * A quiz trail's cards to print: a cover with the player link's QR code,
 * then the cards, two to a sheet; and the host's answer key.
 */
export function TrailPrint({ event }: { event: HostedEvent }) {
  const { i18n, api } = useHosted();
  const t = i18n.t;
  return (
    <div>
      <p className="hosted-muted">{t("Print the cards and hide them in order. The first page has the QR code the teams scan to join.")}</p>
      {event.reprint && <p className="hosted-note">{t("The cards changed since you printed them. Print them again.")}</p>}
      <div className="hosted-actions">
        <a className="hosted-btn hosted-btn--primary hosted-btn--link" href={api.pdf()} target="_blank" rel="noopener">
          {t("Print the cards (PDF)")}
        </a>
        <a className="hosted-btn hosted-btn--link" href={api.pdf(true)} target="_blank" rel="noopener">
          {t("Answer key (PDF)")}
        </a>
      </div>
    </div>
  );
}
