import { useT } from "../../i18n.tsx";
import { errorText } from "../../ui/index.ts";
import { reasonOf } from "./api.ts";

/** A language code by its name in locale, e.g. "sv" as "Swedish". */
export function langName(code: string, locale: string): string {
  try {
    return new Intl.DisplayNames([locale], { type: "language" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/** A country code by its name in locale, e.g. "SE" as "Sweden". */
export function regionName(code: string, locale: string): string {
  try {
    return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/** A partner's text in locale's language, else its English one. */
export const localText = (m: Record<string, string> | null | undefined, locale: string) => m?.[locale.split("-")[0] ?? ""] || m?.en || "";

const REASONS: Record<string, string> = {
  not_pending: "This redemption has already been decided.",
  not_cancellable: "This redemption can no longer be cancelled.",
  event_closed: "The self-guided adventure isn't open, so no team can be created. Open it again, or point the product at another one.",
  code_used: "That code has already been redeemed.",
  invalid_code: "That code doesn't look right for this partner.",
  invalid_email: "Enter a valid email address.",
  missing_field: "Fill in every required field.",
  unknown_product: "Pick a product.",
  invalid_language: "The envelope can't be printed in that language.",
  invalid_country: "Letters aren't posted to that country.",
  exists: "A partner by that name already exists.",
  in_use: "It has redemptions, so it can't be deleted. Disable it instead.",
};

/** The redemption handlers' reason codes as text for the operator; anything else as the server said it. */
export function useRedemptionError() {
  const t = useT();
  return (e: unknown) => {
    const r = reasonOf(e);
    return r && REASONS[r] ? t.t(REASONS[r]) : errorText(e);
  };
}
