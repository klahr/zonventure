import { useState } from "react";
import { X } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Card, ErrorBox, Loading, errorText, useLoad, useToast } from "../../ui/index.ts";
import { getShipCountries, putShipCountries } from "./organization-api.ts";

/** Countries a Swedish organisation is likely to post to, as offered to add. */
const OFFERED = [
  "SE", "NO", "DK", "FI", "IS", "DE", "NL", "BE", "LU", "FR", "GB", "IE", "ES", "PT", "IT", "AT", "CH",
  "PL", "CZ", "SK", "HU", "SI", "HR", "EE", "LV", "LT", "GR", "CY", "MT", "RO", "BG", "US", "CA", "AU", "NZ",
];

function regionName(code: string, locale: string): string {
  try {
    return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/**
 * Where the organisation posts printed letters: a buyer's address must be
 * in one of these, at checkout and on the redeem page, which asks which
 * when there is more than one. Saves at once, for admins.
 */
export function ShipCountriesCard({ canEdit }: { canEdit: boolean }) {
  const t = useT();
  const toast = useToast();
  const locale = t.current();
  const list = useLoad((s) => getShipCountries(s), []);
  const [busy, setBusy] = useState(false);

  const save = async (next: string[]) => {
    setBusy(true);
    try {
      list.set(await putShipCountries(next));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const countries = list.data ?? [];
  const addable = OFFERED.filter((c) => !countries.includes(c))
    .sort((a, b) => regionName(a, locale).localeCompare(regionName(b, locale), locale));

  return (
    <Card title={t.t("Posting letters")} subtitle={t.t("Where you post printed letters to buyers, for adventures that come with one. Buyers give an address in one of these countries.")}>
      {list.error && !list.data ? (
        <ErrorBox message={list.error} retry={list.reload} retryLabel={t.t("Retry")} />
      ) : !list.data ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <div className="ship-countries">
          <ul>
            {countries.map((c) => (
              <li key={c}>
                <span>{regionName(c, locale)}</span>
                {canEdit && countries.length > 1 && (
                  <button type="button" disabled={busy} aria-label={t.t("Remove {{country}}", { country: regionName(c, locale) })}
                    onClick={() => void save(countries.filter((x) => x !== c))}>
                    <X size={14} aria-hidden />
                  </button>
                )}
              </li>
            ))}
          </ul>
          {canEdit && addable.length > 0 && (
            <select value="" disabled={busy} aria-label={t.t("Add a country")} onChange={(e) => e.target.value && void save([...countries, e.target.value])}>
              <option value="">{t.t("+ Add a country…")}</option>
              {addable.map((c) => <option key={c} value={c}>{regionName(c, locale)}</option>)}
            </select>
          )}
        </div>
      )}
    </Card>
  );
}
