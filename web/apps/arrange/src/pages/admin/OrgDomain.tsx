import { useEffect, useState } from "react";
import { Globe } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Field, ICON, Loading, Pill, useLoad, useToast, type Tone } from "../../ui/index.ts";
import { getOrgCustomDomain, putOrgCustomDomain } from "../settings/domain-api.ts";

/** An entitlement with no end date (domains.Forever). */
export const FOREVER = "9999-12-31";

/** A year from today, or from the current end if that is later: a renewal. */
function nextYear(from: string): string {
  const today = new Date();
  const base = from && new Date(from) > today ? new Date(from) : today;
  base.setFullYear(base.getFullYear() + 1);
  return base.toISOString().slice(0, 10);
}

/**
 * The platform admin grants an organisation its own domain until a day,
 * once it has paid, and sees how far its setup has come. Granting it
 * emails the organisation's admins how to start (admin/domains.go).
 */
export function OrgDomainCard({ orgID, readOnly }: { orgID: number; readOnly: boolean }) {
  const t = useT();
  const toast = useToast();
  const load = useLoad((s) => getOrgCustomDomain(orgID, s), [orgID]);
  const [until, setUntil] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (load.data) setUntil(load.data.until);
  }, [load.data]);

  const save = async (value: string) => {
    setBusy(true);
    try {
      load.set(await putOrgCustomDomain(orgID, value));
      toast.ok(value === FOREVER ? t.t("Own domain included with no end date") : value ? t.t("Own domain included until {{date}}", { date: value }) : t.t("Own domain turned off"));
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  const d = load.data;
  const status: Record<string, [string, Tone]> = {
    waiting: [t.t("Waiting for DNS"), "warn"],
    wrong: [t.t("DNS record wrong"), "bad"],
    certificate: [t.t("Getting certificate"), "info"],
    live: [t.t("Live"), "good"],
  };
  const today = new Date().toISOString().slice(0, 10);
  return (
    <Card title={t.t("Own domain")} subtitle={t.t("Grant it once the org has paid. Its admins get an email with how to set it up; after the end date it keeps working for 30 days.")}>
      {!d ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <>
          {d.domain ? (
            <p className="adm-sub">
              <Globe size={ICON.sm} aria-hidden /> <a href={`https://${d.domain.host}`} target="_blank" rel="noopener">{d.domain.host}</a>{" "}
              {d.until ? (
                <Pill tone={status[d.domain.status]?.[1] ?? "neutral"}>{status[d.domain.status]?.[0] ?? d.domain.status}</Pill>
              ) : (
                <Pill tone="neutral">{t.t("Off: links go back to our address")}</Pill>
              )}
            </p>
          ) : d.until ? (
            <p className="adm-sub">{t.t("They haven't chosen an address yet.")}</p>
          ) : null}
          {d.until === FOREVER ? (
            <div className="cs-row">
              <p className="adm-sub">{t.t("Included with no end date: never billed, never ends.")}</p>
              {!readOnly && <Button variant="ghost" onClick={() => void save("")} disabled={busy}>{t.t("Turn off")}</Button>}
            </div>
          ) : (
            <form className="cs-row adm-domain-row" onSubmit={(e) => { e.preventDefault(); void save(until); }}>
              <Field label={t.t("Included until")} hint={d.until && d.until < today ? t.t("Ended: in its 30 days of grace, then off.") : undefined}>
                <input type="date" className="st-date" value={until} disabled={readOnly} onChange={(e) => setUntil(e.target.value)} />
              </Field>
              {!readOnly && (
                <>
                  <Button type="submit" variant="primary" loading={busy} disabled={until === d.until}>{t.t("Save")}</Button>
                  <Button onClick={() => void save(nextYear(d.until))} disabled={busy}>{d.until ? t.t("Renew a year") : t.t("Grant a year")}</Button>
                  <Button variant="ghost" onClick={() => void save(FOREVER)} disabled={busy} title={t.t("For the platform's own organization, or a partner you don't bill.")}>{t.t("No end date")}</Button>
                  {d.until && <Button variant="ghost" onClick={() => void save("")} disabled={busy}>{t.t("Turn off")}</Button>}
                </>
              )}
            </form>
          )}
        </>
      )}
    </Card>
  );
}
