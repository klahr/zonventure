import { useState } from "react";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, Field, useToast } from "../../ui/index.ts";
import { addProduct, createRedemption, updateProduct, type Partner, type PartnerProduct, type Redemption, type SelfGuidedEvent } from "./api.ts";
import { langName, partnerLabel, regionName, useRedemptionError } from "./shared.ts";

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
      <label className="rd-check">
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

/** Manual entry, for a box redeemed by phone or email. It lands as pending, like one from the public page. */
export function AddRedemptionDialog({ partners, onClose, onDone }: { partners: Partner[]; onClose: () => void; onDone: () => void }) {
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
  const liveit = picked?.partner.kind === "liveit";
  // A product whose event posts nothing needs no address.
  const letter = picked?.letter ?? null;
  const langs = letter?.languages ?? [];
  const countries = letter?.countries ?? [];
  const [lang, setLang] = useState("");
  const [country, setCountry] = useState("");

  const submit = async () => {
    setBusy(true);
    try {
      await createRedemption({
        product_id: picked ? picked.id : 0,
        box_number: liveit ? f.box.trim() : "",
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
      toast.ok(t.t("Redemption added"));
      onDone();
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
            <optgroup key={p.id} label={partnerLabel(p.kind, p.name)}>
              {p.products.map((x) => <option key={x.id} value={x.id}>{x.name || x.adventure_title || x.event_name}</option>)}
            </optgroup>
          ))}
        </select>
      </Field>
      <div className="rd-grid">
        {liveit && (
          <Field label={t.t("Box number")}>
            <input type="text" inputMode="numeric" pattern="[0-9]*" autoComplete="off" required value={f.box} onChange={set("box")} />
          </Field>
        )}
        <Field label={liveit ? t.t("Security code") : t.t("Experience code")}>
          <input type="text" required autoComplete="off" className="rd-mono" value={f.code} onChange={set("code")} {...(liveit ? {} : { minLength: 9, maxLength: 9 })} />
        </Field>
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
      title={existing ? t.t("Edit product") : t.t("Add product to {{partner}}", { partner: partnerLabel(partner.kind, partner.name) })}
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
