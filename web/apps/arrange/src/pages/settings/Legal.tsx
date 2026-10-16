import { useEffect, useState } from "react";
import { csrfToken, get, put } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Callout, Card, ErrorBox, Field, Loading, Row, errorText, useLoad, useToast } from "../../ui/index.ts";

/** settings.Legal, internal/settings/legal.go. */
interface Legal {
  company_name: string;
  company_org_number: string;
  company_address: string;
  company_vat_number: string;
  contact_email: string;
  dpo_email: string;
  jurisdiction: string;
  hosting_provider: string;
  email_provider: string;
  location_retention_days: number;
  audit_retention_days: number;
  party_retention_days: number;
  address_retention_days: number;
  reviewed: boolean;
  last_updated: string;
}

type TextKey = { [K in keyof Legal]: Legal[K] extends string ? K : never }[keyof Legal];
type DaysKey = "location_retention_days" | "audit_retention_days" | "party_retention_days" | "address_retention_days";

/**
 * Who runs the platform, how to reach them and how long data is kept,
 * for the platform admin: the blanks of /privacy and /terms, the seller
 * line by every buy button, and the retention sweepers' periods. The
 * pages are previewed with the values as typed; one Save applies them.
 */
export function LegalTab() {
  const t = useT();
  const load = useLoad((signal) => get<Legal>("/api/admin/legal", { signal }), []);
  const [draft, setDraft] = useState<Legal | null>(null);
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (load.data) setDraft(load.data);
  }, [load.data]);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data || !draft) return <Loading label={t.t("Loading…")} />;
  const saved = load.data;
  const changed = JSON.stringify(draft) !== JSON.stringify(saved);
  const missing = [
    !draft.company_name.trim() && t.t("Company name"),
    !draft.contact_email.trim() && t.t("Contact email"),
    !draft.jurisdiction.trim() && t.t("Jurisdiction"),
  ].filter((x): x is string => !!x);

  const text = (k: TextKey, label: string, opts: { hint?: string; type?: string; placeholder?: string; required?: boolean } = {}) => (
    <Field label={opts.required ? `${label} *` : label} {...(opts.hint ? { hint: opts.hint } : {})}>
      <input type={opts.type ?? "text"} maxLength={300} value={draft[k]} placeholder={opts.placeholder} onChange={(e) => setDraft({ ...draft, [k]: e.target.value })} />
    </Field>
  );
  const days = (k: DaysKey, label: string) => (
    <Field label={label}>
      <input type="number" min={0} max={3650} value={draft[k]} className="st-num" onChange={(e) => setDraft({ ...draft, [k]: Math.max(0, Math.trunc(Number(e.target.value) || 0)) })} />
    </Field>
  );
  const save = async () => {
    setBusy(true);
    try {
      load.set(await put<Legal>("/api/admin/legal", draft));
      toast.ok(t.t("Saved"));
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="st-legal">
      <div className="st-legal__form">
        {missing.length > 0 ? (
          <Callout variant="warning">{t.t("Privacy and terms are not published yet. Fill in: {{missing}}.", { missing: missing.join(", ") })}</Callout>
        ) : (
          <p className="cs-field__hint cs-flush">
            <a href="/privacy" target="_blank" rel="noopener">{t.t("Privacy policy ↗")}</a> · <a href="/terms" target="_blank" rel="noopener">{t.t("Terms ↗")}</a>
          </p>
        )}
        <Card title={t.t("Company")} subtitle={t.t("Shown on the legal pages and as the seller next to every buy button.")}>
          {text("company_name", t.t("Company name"), { required: true, placeholder: t.t("e.g. {{example}}", { example: t.t("Your Company AB") }) })}
          <Row>
            {text("company_org_number", t.t("Organisation number"), { placeholder: t.t("e.g. {{example}}", { example: "556123-4567" }) })}
            {text("company_vat_number", t.t("VAT number"), { placeholder: t.t("e.g. {{example}}", { example: "SE556123456701" }) })}
          </Row>
          {text("company_address", t.t("Address"))}
        </Card>
        <Card title={t.t("Contact")}>
          <Row>
            {text("contact_email", t.t("Contact email"), { required: true, type: "email" })}
            {text("dpo_email", t.t("Data protection email"), { type: "email", hint: t.t("Blank uses the contact email.") })}
          </Row>
          {text("jurisdiction", t.t("Jurisdiction"), { required: true, placeholder: t.t("e.g. {{example}}", { example: t.t("Sweden") }), hint: t.t("Whose courts settle disputes.") })}
        </Card>
        <Card title={t.t("Sub-processors")} subtitle={t.t("Named in the privacy policy.")}>
          <Row>
            {text("hosting_provider", t.t("Hosting provider"), { placeholder: t.t("e.g. {{example}}", { example: t.t("your hosting company") }) })}
            {text("email_provider", t.t("Email provider"))}
          </Row>
        </Card>
        <Card title={t.t("Data kept")} subtitle={t.t("Days before it is deleted; 0 keeps it. The privacy policy says the same, and the cleanup uses these from its next run.")}>
          <Row>
            {days("location_retention_days", t.t("Player locations and answers"))}
            {days("audit_retention_days", t.t("Audit logs (login history)"))}
            {days("party_retention_days", t.t("A finished party adventure"))}
            {days("address_retention_days", t.t("Postal addresses for letters we post"))}
          </Row>
        </Card>
        <Card title={t.t("Review")}>
          <label className="cs-row cs-row--sm">
            <input type="checkbox" checked={draft.reviewed} onChange={(e) => setDraft({ ...draft, reviewed: e.target.checked })} />
            <span>{t.t("Reviewed by a lawyer: remove the draft notes from both pages")}</span>
          </label>
          <Field label={t.t("Last updated")} hint={t.t("Moves to today when a save changes what the pages say.")}>
            <input type="date" value={draft.last_updated} className="st-date" onChange={(e) => setDraft({ ...draft, last_updated: e.target.value })} />
          </Field>
        </Card>
        <Row>
          <Button variant="primary" loading={busy} disabled={!changed} onClick={() => void save()}>
            {t.t("Save")}
          </Button>
        </Row>
      </div>
      <LegalPreview legal={draft} />
    </div>
  );
}

/**
 * The sandboxed preview can't load the self-hosted fonts (opaque origin), and the pages' body stack
 * then falls back to an emoji font whose wide digits and spaces look like justified text.
 */
const previewFonts = "<style>:root{--font-body:system-ui,-apple-system,'Segoe UI',sans-serif}</style>";

/** The privacy policy or the terms as the typed values make them, a moment after the last keystroke. */
function LegalPreview({ legal }: { legal: Legal }) {
  const t = useT();
  const [page, setPage] = useState<"privacy" | "terms">("privacy");
  const [html, setHtml] = useState("");
  const [error, setError] = useState<string | null>(null);
  const body = JSON.stringify({ page, legal });
  useEffect(() => {
    const ac = new AbortController();
    const timer = setTimeout(() => {
      fetch("/api/admin/legal/preview", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
        body,
        signal: ac.signal,
      })
        .then((r) => (r.ok ? r.text() : Promise.reject(new Error(r.statusText))))
        .then((h) => {
          setHtml(h.replace("</head>", previewFonts + "</head>"));
          setError(null);
        })
        .catch((e: unknown) => !ac.signal.aborted && setError(errorText(e)));
    }, 400);
    return () => {
      clearTimeout(timer);
      ac.abort();
    };
  }, [body]);
  return (
    <aside className="st-legal__preview" aria-label={t.t("Preview")}>
      <div className="cs-row cs-row--sm" role="tablist">
        {(["privacy", "terms"] as const).map((p) => (
          <Button key={p} size="sm" variant={p === page ? "primary" : "ghost"} role="tab" aria-selected={p === page} onClick={() => setPage(p)}>
            {p === "privacy" ? t.t("Privacy policy") : t.t("Terms")}
          </Button>
        ))}
      </div>
      {error ? <ErrorBox message={error} /> : <iframe title={t.t("Preview")} srcDoc={html} sandbox="" className="st-legal__frame" />}
    </aside>
  );
}
