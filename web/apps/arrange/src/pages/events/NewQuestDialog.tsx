import { useEffect, useState } from "react";
import { useT } from "../../i18n.tsx";
import { CATALOG_ENABLED, navigate } from "../../router.ts";
import { Button, Dialog, ErrorBox, Field, ImageDrop, Loading, errorText, useLoad, useToast } from "../../ui/index.ts";
import { createEvent, listAdventures, startEvent, updateEvent, uploadEventCover } from "./api.ts";
import { AdventureCover } from "./parts.tsx";

const CURRENCIES = ["EUR", "USD", "SEK", "GBP", "NOK", "DKK"];

/**
 * A new self-guided adventure: an always-on event sold per ticket in
 * the org's store. Tickets pay for it, so no event pass is needed —
 * only an adventure the org owns or was commissioned. Created, priced,
 * given its cover and started in one go, as the classic console does.
 * Given a slug, it is for that adventure only.
 */
export function NewQuestDialog({ slug, onClose, onCreated }: { slug?: string; onClose: () => void; onCreated: (id: number) => void }) {
  const t = useT();
  const toast = useToast();
  const advs = useLoad((signal) => listAdventures(signal).then((l) => l.filter((a) => (a.mine_to_edit || a.shared) && (!slug || a.slug === slug))), [slug]);
  const [picked, setPicked] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [blurb, setBlurb] = useState("");
  const [price, setPrice] = useState("20");
  const [currency, setCurrency] = useState("EUR");
  const [teamSize, setTeamSize] = useState("4");
  const [cover, setCover] = useState<File | null>(null);
  const [coverSrc, setCoverSrc] = useState<string | undefined>(undefined);
  useEffect(() => {
    if (!cover) return setCoverSrc(undefined);
    const url = URL.createObjectURL(cover);
    setCoverSrc(url);
    return () => URL.revokeObjectURL(url);
  }, [cover]);
  const [attest, setAttest] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!advs.data) return;
    if (advs.data.length === 0) {
      toast.err(t.t("Create or browse an adventure first"));
      onClose();
      return;
    }
    setPicked((p) => p ?? advs.data![0]!.slug);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [advs.data]);

  async function submit() {
    if (busy || !picked) return;
    setError(null);
    if (!name.trim()) return setError(t.t("A name is required"));
    const priceN = parseInt(price, 10);
    if (!priceN || priceN <= 0) return setError(t.t("Ticket price must be greater than zero"));
    const teamN = parseInt(teamSize, 10);
    if (!teamN || teamN < 1) return setError(t.t("Players per ticket must be at least 1"));
    if (!attest) return setError(t.t("Confirm the no-minors attestation"));

    setBusy(true);
    let id: number;
    try {
      id = (await createEvent({ adventure_slug: picked, name: name.trim(), attest_no_minors: true, public_for_sale: true })).id;
    } catch (e) {
      setError(errorText(e) || t.t("Could not create the self-guided adventure"));
      setBusy(false);
      return;
    }
    // A failure from here on leaves a quest to finish on its own page,
    // rather than one to clean up.
    try {
      await updateEvent(id, { public_blurb: blurb.trim(), ticket_price_cents: priceN * 100, ticket_currency: currency, ticket_team_size: teamN });
    } catch {
      toast.err(t.t("Self-guided adventure created, but its ticket details were not saved"));
      onClose();
      navigate({ name: "event", id });
      return;
    }
    if (cover) {
      try {
        await uploadEventCover(id, cover);
      } catch {
        toast.err(t.t("Self-guided adventure created, but the cover upload failed"));
      }
    }
    try {
      await startEvent(id);
    } catch {
      toast.err(t.t("Self-guided adventure created, but it could not start"));
      onClose();
      navigate({ name: "event", id });
      return;
    }
    toast.ok(t.t("Self-guided adventure created and listed in your store"));
    onClose();
    onCreated(id);
  }

  if (!advs.data) {
    return (
      <Dialog title={t.t("New self-guided adventure")} onClose={onClose} footer={advs.error ? <Button onClick={onClose}>{t.t("Cancel")}</Button> : undefined}>
        {advs.error ? <ErrorBox message={advs.error} retry={advs.reload} retryLabel={t.t("Retry")} /> : <Loading label={t.t("Loading…")} />}
      </Dialog>
    );
  }

  return (
    <Dialog
      title={t.t("New self-guided adventure")}
      wide
      onClose={() => !busy && onClose()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Create self-guided adventure")}</Button>
        </>
      }
    >
      {advs.data.length === 1 ? (
        <div className="evs-chosen">
          <AdventureCover title={advs.data[0]!.title} src={advs.data[0]!.avatar_url} size={36} />
          <strong>{advs.data[0]!.title}</strong>
        </div>
      ) : (
        <fieldset className="evs-fieldset">
          <legend className="cs-field__label">{t.t("Adventure")}</legend>
          <div className="evs-picker" role="radiogroup" aria-label={t.t("Adventure")}>
            {advs.data.map((a) => (
              <label key={a.slug} className={"evs-choice" + (a.slug === picked ? " is-on" : "")}>
                <input type="radio" name="evs-quest-adventure" checked={a.slug === picked} onChange={() => setPicked(a.slug)} />
                <AdventureCover title={a.title} src={a.avatar_url} />
                <span className="evs-stack">
                  <strong className="evs-ellipsis">{a.title}</strong>
                  <span className="evs-muted evs-small">{a.mine_to_edit ? t.t("yours") : CATALOG_ENABLED ? t.t("catalog") : t.t("shared")}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <Field label={t.t("Name in the store")}>
        <input required maxLength={200} value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t.t("Public description")}>
        <textarea rows={4} maxLength={1000} value={blurb} onChange={(e) => setBlurb(e.target.value)} placeholder={t.t("Short description shown in your store.")} />
      </Field>
      <div className="evs-grid3">
        <Field label={t.t("Ticket price")}>
          <input type="number" min={1} step={1} value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
        <Field label={t.t("Currency")}>
          <select value={currency} onChange={(e) => setCurrency(e.target.value)}>
            {CURRENCIES.map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </Field>
        <Field label={t.t("Players per ticket")}>
          <input type="number" min={1} max={20} value={teamSize} onChange={(e) => setTeamSize(e.target.value)} />
        </Field>
      </div>
      <div className="cs-field">
        <span className="cs-field__label">{t.t("Cover image (optional)")}</span>
        <ImageDrop
          shape="cover"
          src={coverSrc}
          label={t.t("Upload cover")}
          title={t.t("Click to upload a cover")}
          accept="image/*"
          onFile={setCover}
          onRemove={() => setCover(null)}
        />
        <span className="cs-field__hint">{t.t("Shown in your store. JPEG / PNG / WebP / SVG, max 5 MB.")}</span>
      </div>
      <label className="evs-check evs-attest">
        <input type="checkbox" checked={attest} onChange={(e) => setAttest(e.target.checked)} />
        <span>{t.t("I confirm there are no under-13 players (or that I have parental consent for any).")}</span>
      </label>
      {error && <ErrorBox message={error} />}
    </Dialog>
  );
}
