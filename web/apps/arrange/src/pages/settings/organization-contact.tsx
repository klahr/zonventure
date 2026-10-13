import { useEffect, useState } from "react";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Field, Loading, errorText, useLoad, useToast } from "../../ui/index.ts";
import { getContact, putContact } from "./organization-api.ts";

/** Every time zone the browser knows, the saved one kept when it is not among them. */
function zones(saved: string): string[] {
  let all: string[] = [];
  try {
    all = Intl.supportedValuesOf("timeZone");
  } catch {
    all = [];
  }
  return all.includes(saved) ? all : [saved, ...all];
}

/**
 * How buyers and teams reach the organisation, and which clock it keeps:
 * the contact address is the Reply-To of its mails and is shown on
 * tickets, cancellations and rejections; the time zone is the one
 * invitations and the results export write times in. Admins edit.
 */
export function ContactCard({ canEdit }: { canEdit: boolean }) {
  const t = useT();
  const toast = useToast();
  const load = useLoad((s) => getContact(s), []);
  const [email, setEmail] = useState("");
  const [tz, setTz] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (load.data) {
      setEmail(load.data.contact_email);
      setTz(load.data.time_zone);
    }
  }, [load.data]);

  const save = async () => {
    setBusy(true);
    try {
      load.set(await putContact({ contact_email: email.trim(), time_zone: tz }));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const d = load.data;
  return (
    <Card title={t.t("Contact and time zone")} subtitle={t.t("Buyers and teams reply to this address, and tickets, cancellations and rejections show it. Start times in invitations and the results export are written in this time zone.")}>
      {load.error && !d ? (
        <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />
      ) : !d ? (
        <Loading label={t.t("Loading…")} />
      ) : (
        <form onSubmit={(e) => { e.preventDefault(); void save(); }}>
          <Field label={t.t("Contact email")} hint={t.t("Empty uses your first admin's address: {{email}}", { email: d.default_contact_email || "—" })}>
            <input type="email" value={email} disabled={!canEdit} maxLength={254} placeholder={d.default_contact_email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label={t.t("Time zone")}>
            <select value={tz} disabled={!canEdit} onChange={(e) => setTz(e.target.value)}>
              {zones(d.time_zone).map((z) => <option key={z} value={z}>{z.replace(/_/g, " ")}</option>)}
            </select>
          </Field>
          {canEdit && (
            <Button type="submit" variant="primary" loading={busy} disabled={email.trim() === d.contact_email && tz === d.time_zone}>{t.t("Save")}</Button>
          )}
        </form>
      )}
    </Card>
  );
}
