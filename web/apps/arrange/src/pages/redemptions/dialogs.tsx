import { useState } from "react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, Field, useToast } from "../../ui/index.ts";
import { addPartner, addProduct, createRedemption, updatePartner, updateProduct, type Partner, type PartnerField, type PartnerProduct, type PartnerSetup, type Redemption, type SelfGuidedEvent } from "./api.ts";
import { langName, localText, regionName, useRedemptionError } from "./shared.ts";

/** Rejecting a code, with an optional reason the customer's email passes on. */
export function RejectDialog({ email, onClose, onReject }: { email: string; onClose: () => void; onReject: (reason: string) => void }) {
  const t = useT();
  const [reason, setReason] = useState("");
  return (
    <Dialog
      title={t.t("Reject this redemption?")}
      onClose={onClose}
      onSubmit={() => onReject(reason.trim())}
      footer={<><Button onClick={onClose}>{t.t("Back")}</Button><Button type="submit" variant="danger" filled>{t.t("Reject")}</Button></>}
    >
      <p className="confirm-message">{t.t("{{email}} is told by email that the code couldn't be verified.", { email })}</p>
      <Field label={t.t("Reason (optional)")} hint={t.t("Included in the email to the customer.")}>
        <textarea rows={3} maxLength={1000} value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />
      </Field>
    </Dialog>
  );
}

export function PostDialog({ rd, onClose, onSave }: { rd: Redemption; onClose: () => void; onSave: (b: { post_at: string; posted: boolean; note: string; letter_lang?: string }) => void }) {
  const t = useT();
  const [postAt, setPostAt] = useState(rd.post_at || "");
  const [posted, setPosted] = useState(!!rd.posted_at);
  const [note, setNote] = useState(rd.note || "");
  const langs = rd.letter_languages ?? [];
  const [lang, setLang] = useState(rd.letter_lang || langs[0] || "");
  return (
    <Dialog
      title={t.t("Post date and note")}
      onClose={onClose}
      onSubmit={() => onSave({ post_at: postAt, posted, note: note.trim(), ...(langs.length > 1 ? { letter_lang: lang } : {}) })}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary">{t.t("Save")}</Button></>}
    >
      <Field label={t.t("Post on")}>
        <input type="date" value={postAt} onChange={(e) => setPostAt(e.target.value)} autoFocus />
      </Field>
      <label className="cs-check">
        <input type="checkbox" checked={posted} onChange={(e) => setPosted(e.target.checked)} />
        <span>{t.t("Posted")}</span>
      </label>
      {langs.length > 1 && <LetterLangField langs={langs} value={lang} onChange={setLang} />}
      <Field label={t.t("Note")}>
        <textarea rows={4} maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} />
      </Field>
    </Dialog>
  );
}

function LetterLangField({ langs, value, onChange }: { langs: string[]; value: string; onChange: (v: string) => void }) {
  const t = useT();
  return (
    <Field label={t.t("Envelope language")}>
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {langs.map((l) => <option key={l} value={l}>{langName(l, t.current())}</option>)}
      </select>
    </Field>
  );
}

/** Manual entry, for a box redeemed by phone or email. Staff entries are approved at once; one whose event is closed stays pending. */
export function AddRedemptionDialog({ partners, onClose, onDone }: { partners: Partner[]; onClose: () => void; onDone: (state: Redemption["state"]) => void }) {
  const t = useT();
  const toast = useToast();
  const errText = useRedemptionError();
  const enabled = partners.filter((p) => p.enabled).map((p) => ({ ...p, products: (p.products ?? []).filter((x) => x.enabled) })).filter((p) => p.products.length > 0);
  const products = enabled.flatMap((p) => p.products.map((x) => ({ ...x, partner: p })));
  const [productId, setProductId] = useState(String(products[0]?.id ?? ""));
  const [f, setF] = useState({ box: "", code: "", name: "", email: "", street: "", address2: "", postal: "", city: "", postAt: "", note: "" });
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF((v) => ({ ...v, [k]: e.target.value }));
  const picked = products.find((x) => String(x.id) === productId);
  // Two fields are box number then security code; one is the code alone.
  const fields = picked?.partner.fields ?? [];
  const two = fields.length === 2;
  const codeField = fields[fields.length - 1];
  // A product whose event posts nothing needs no address.
  const letter = picked?.letter ?? null;
  const langs = letter?.languages ?? [];
  const countries = letter?.countries ?? [];
  const [lang, setLang] = useState("");
  const [country, setCountry] = useState("");

  const submit = async () => {
    setBusy(true);
    try {
      const rd = await createRedemption({
        product_id: picked ? picked.id : 0,
        box_number: two ? f.box.trim() : "",
        security_code: f.code.trim(),
        name: f.name.trim(),
        email: f.email.trim(),
        street_address: f.street.trim(),
        address_2: f.address2.trim(),
        postal_code: f.postal.trim(),
        city: f.city.trim(),
        country: countries.includes(country) ? country : countries[0] ?? "",
        letter_lang: langs.includes(lang) ? lang : langs[0] ?? "",
        post_at: f.postAt,
        note: f.note.trim(),
      });
      toast.ok(rd.state === "approved" ? t.t("Redemption added and approved; its team link is emailed") : t.t("Redemption added"));
      onDone(rd.state);
    } catch (e) {
      toast.err(errText(e));
      setBusy(false);
    }
  };

  return (
    <Dialog
      wide
      title={t.t("Add redemption")}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Add redemption")}</Button></>}
    >
      <Field label={t.t("Product")}>
        <select value={productId} onChange={(e) => setProductId(e.target.value)} required autoFocus>
          {enabled.map((p) => (
            <optgroup key={p.id} label={p.name}>
              {p.products.map((x) => <option key={x.id} value={x.id}>{x.name || x.adventure_title || x.event_name}</option>)}
            </optgroup>
          ))}
        </select>
      </Field>
      <div className="rd-grid">
        {two && <CodeInput field={fields[0]!} value={f.box} onChange={set("box")} />}
        {codeField && <CodeInput field={codeField} value={f.code} onChange={set("code")} />}
      </div>
      <Field label={t.t("Name")}><input type="text" required maxLength={200} value={f.name} onChange={set("name")} /></Field>
      <Field label={t.t("Email")}><input type="email" required maxLength={254} value={f.email} onChange={set("email")} /></Field>
      {letter && (
        <>
          <Field label={t.t("Street address")}><input type="text" required maxLength={200} autoComplete="street-address" value={f.street} onChange={set("street")} /></Field>
          <Field label={t.t("Address line 2 (optional)")}><input type="text" maxLength={200} value={f.address2} onChange={set("address2")} /></Field>
          <div className="rd-grid rd-grid--postal">
            <Field label={t.t("Postal code")}><input type="text" required maxLength={20} autoComplete="postal-code" value={f.postal} onChange={set("postal")} /></Field>
            <Field label={t.t("City")}><input type="text" required maxLength={100} value={f.city} onChange={set("city")} /></Field>
          </div>
          {countries.length > 1 && (
            <Field label={t.t("Country")}>
              <select value={countries.includes(country) ? country : countries[0]} onChange={(e) => setCountry(e.target.value)}>
                {countries.map((c) => <option key={c} value={c}>{regionName(c, t.current())}</option>)}
              </select>
            </Field>
          )}
          {langs.length > 1 && <LetterLangField langs={langs} value={langs.includes(lang) ? lang : langs[0] ?? ""} onChange={setLang} />}
        </>
      )}
      <Field label={t.t("Post on (optional)")}><input type="date" value={f.postAt} onChange={set("postAt")} /></Field>
      <Field label={t.t("Note (optional)")}><textarea rows={3} maxLength={2000} value={f.note} onChange={set("note")} /></Field>
    </Dialog>
  );
}

const eventLabel = (e: SelfGuidedEvent) => (e.name || `Event #${e.id}`) + (e.adventure_title ? " — " + e.adventure_title : "");

/** Add (existing null) or edit a partner product: its name on the redeem page, the self-guided adventure it unlocks, the team size. */
export function ProductDialog({ partner, existing, events, onClose, onDone }: { partner: Partner; existing: PartnerProduct | null; events: SelfGuidedEvent[]; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const errText = useRedemptionError();
  const options = existing && !events.some((e) => e.id === existing.event_id)
    ? [{ id: existing.event_id, name: existing.event_name || `#${existing.event_id}`, state: "" } as SelfGuidedEvent, ...events]
    : events;
  const [name, setName] = useState(existing?.name ?? "");
  const [eventId, setEventId] = useState(String(existing?.event_id ?? options[0]?.id ?? ""));
  const sizeFor = (id: string) => events.find((e) => String(e.id) === id)?.ticket_team_size || 4;
  const [size, setSize] = useState(String(existing?.team_size || sizeFor(eventId)));
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const n = Number.parseInt(size, 10);
    if (!n || n < 1 || n > 50) { toast.err(t.t("Players per team must be between 1 and 50")); return; }
    const body = { name: name.trim(), event_id: Number.parseInt(eventId, 10), team_size: n };
    setBusy(true);
    try {
      if (existing) await updateProduct(existing.id, body);
      else await addProduct(partner.id, body);
      toast.ok(existing ? t.t("Saved") : t.t("Product added"));
      onDone();
    } catch (e) {
      toast.err(errText(e));
      setBusy(false);
    }
  };

  return (
    <Dialog
      title={existing ? t.t("Edit product") : t.t("Add product to {{partner}}", { partner: partner.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{existing ? t.t("Save") : t.t("Add product")}</Button></>}
    >
      <Field label={t.t("Product name")}>
        <input type="text" required maxLength={200} value={name} placeholder={t.t("As printed on the box")} onChange={(e) => setName(e.target.value)} autoFocus />
      </Field>
      <Field label={t.t("Self-guided adventure")}>
          <select required value={eventId} onChange={(e) => { setEventId(e.target.value); if (!existing) setSize(String(sizeFor(e.target.value))); }}>
            {options.map((e) => <option key={e.id} value={e.id}>{eventLabel(e)}</option>)}
          </select>
      </Field>
      <Field label={t.t("Players per team")}>
        <input type="number" min={1} max={50} required value={size} onChange={(e) => setSize(e.target.value)} />
      </Field>
    </Dialog>
  );
}

/** A partner's code input, named and shaped by its field. */
function CodeInput({ field, value, onChange }: { field: PartnerField; value: string; onChange: (e: { target: { value: string } }) => void }) {
  const t = useT();
  return (
    <Field label={localText(field.label, t.current())}>
      <input type="text" required autoComplete="off" className="cs-mono" inputMode={field.digits ? "numeric" : "text"} placeholder={field.example} value={value} onChange={onChange} />
    </Field>
  );
}

/** The languages a partner's labels and hint are written in (partner_fields.go partnerLangs). */
const PARTNER_LANGS = ["en", "sv"];

const blankField = (): PartnerField => ({ label: {}, example: "", min: 1, max: 20, digits: false });

/** Add (existing null) or set up a partner: its name, the one or two codes its boxes carry, and a hint for buyers. */
export function PartnerDialog({ existing, onClose, onDone }: { existing: Partner | null; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const errText = useRedemptionError();
  const [name, setName] = useState(existing?.name ?? "");
  const [fields, setFields] = useState<PartnerField[]>(existing?.fields?.length ? existing.fields : [blankField()]);
  const [hint, setHint] = useState<Record<string, string>>(existing?.hint ?? {});
  const [contactName, setContactName] = useState(existing?.contact_name ?? "");
  const [contactEmail, setContactEmail] = useState(existing?.contact_email ?? "");
  const [portalURL, setPortalURL] = useState(existing?.portal_url ?? "");
  const [busy, setBusy] = useState(false);
  const setField = (i: number, patch: Partial<PartnerField>) => setFields((fs) => fs.map((f, j) => (j === i ? { ...f, ...patch } : f)));
  const langLabel = (l: string) => langName(l, t.current());

  const submit = async () => {
    const body: PartnerSetup = { name: name.trim(), fields, hint, contact_name: contactName.trim(), contact_email: contactEmail.trim(), portal_url: portalURL.trim() };
    setBusy(true);
    try {
      if (existing) await updatePartner(existing.id, body);
      else await addPartner(body);
      toast.ok(existing ? t.t("Saved") : t.t("Partner added"));
      onDone();
    } catch (e) {
      toast.err(errText(e));
      setBusy(false);
    }
  };

  return (
    <Dialog
      wide
      title={existing ? t.t("Edit {{partner}}", { partner: existing.name }) : t.t("Add partner")}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{existing ? t.t("Save") : t.t("Add partner")}</Button></>}
    >
      <Field label={t.t("Partner name")} hint={t.t("As buyers know it, such as the company that sold the box.")}>
        <input type="text" required maxLength={60} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </Field>
      <Field label={t.t("Codes on each box")}>
        <select value={fields.length} onChange={(e) => setFields((fs) => (e.target.value === "2" ? [fs[0]!, fs[1] ?? blankField()] : [fs[fs.length - 1]!]))}>
          <option value={1}>{t.t("One code")}</option>
          <option value={2}>{t.t("Two codes, such as a box number and a security code")}</option>
        </select>
      </Field>
      {fields.map((f, i) => (
        <fieldset key={i} className="rd-fieldset">
          <legend>{fields.length === 2 ? t.t("Code {{n}}", { n: i + 1 }) : t.t("Code")}</legend>
          <div className="rd-grid">
            {PARTNER_LANGS.map((l) => (
              <Field key={l} label={t.t("Label in {{language}}", { language: langLabel(l) })} hint={l === "en" ? t.t("What buyers are asked for, such as “Box number (on the back of the box)”.") : t.t("Empty uses the English one.")}>
                <input type="text" required={l === "en"} maxLength={60} value={f.label[l] ?? ""} onChange={(e) => setField(i, { label: { ...f.label, [l]: e.target.value } })} />
              </Field>
            ))}
          </div>
          <div className="rd-grid">
            <Field label={t.t("Example (optional)")} hint={t.t("Shown greyed out in the empty input.")}>
              <input type="text" maxLength={64} className="cs-mono" value={f.example} onChange={(e) => setField(i, { example: e.target.value })} />
            </Field>
            <Field label={t.t("Characters")}>
              <select value={f.digits ? "digits" : "alnum"} onChange={(e) => setField(i, { digits: e.target.value === "digits" })}>
                <option value="alnum">{t.t("Letters and digits")}</option>
                <option value="digits">{t.t("Digits only")}</option>
              </select>
            </Field>
            <Field label={t.t("Shortest")}>
              <input type="number" min={1} max={64} required value={f.min} onChange={(e) => setField(i, { min: Number.parseInt(e.target.value, 10) || 0 })} />
            </Field>
            <Field label={t.t("Longest")} hint={t.t("Spaces and dashes don't count.")}>
              <input type="number" min={1} max={64} required value={f.max} onChange={(e) => setField(i, { max: Number.parseInt(e.target.value, 10) || 0 })} />
            </Field>
          </div>
        </fieldset>
      ))}
      <div className="rd-grid">
        {PARTNER_LANGS.map((l) => (
          <Field key={l} label={t.t("Hint in {{language}} (optional)", { language: langLabel(l) })} hint={l === "en" ? t.t("Where buyers find the codes, shown under them.") : t.t("Empty uses the English one.")}>
            <textarea rows={2} maxLength={300} value={hint[l] ?? ""} onChange={(e) => setHint((h) => ({ ...h, [l]: e.target.value }))} />
          </Field>
        ))}
      </div>
      <fieldset className="rd-fieldset">
        <legend>{t.t("At the partner (optional)")}</legend>
        <p className="cs-note">{t.t("Only your organization sees these, never buyers.")}</p>
        <Field label={t.t("Partner portal")} hint={t.t("Where you check their codes. Each code to review links to it.")}>
          <input type="url" maxLength={500} placeholder="https://" value={portalURL} onChange={(e) => setPortalURL(e.target.value)} />
        </Field>
        <div className="rd-grid">
          <Field label={t.t("Contact person")}>
            <input type="text" maxLength={200} autoComplete="off" value={contactName} onChange={(e) => setContactName(e.target.value)} />
          </Field>
          <Field label={t.t("Contact email")}>
            <input type="email" maxLength={200} autoComplete="off" value={contactEmail} onChange={(e) => setContactEmail(e.target.value)} />
          </Field>
        </div>
      </fieldset>
    </Dialog>
  );
}
