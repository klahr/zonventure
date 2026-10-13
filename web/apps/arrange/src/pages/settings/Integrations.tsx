import { useRef, useState } from "react";
import { Copy, FastForward, KeyRound, Pencil, Plus, Power, ScrollText, Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Card, Dialog, Empty, ErrorBox, Field, HelpPopover, ICON, Loading, Pill, errorText, fmtDateTime, relTime, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { MoreMenu } from "../event/Menu.tsx";
import { createWebhook, deleteWebhook, listKinds, listWebhooks, rotateSecret, testWebhook, updateWebhook, type WebhookEndpoint } from "./integrations-api.ts";
import "./integrations-webhooks.css";

async function loadAll(signal: AbortSignal) {
  const [eps, kinds] = await Promise.all([listWebhooks(signal), listKinds(signal).catch(() => ({ event_kinds: [] }))]);
  return { endpoints: eps.endpoints ?? [], kinds: kinds.event_kinds ?? [] };
}

/**
 * Outbound webhooks: endpoints with the lifecycle events they get,
 * signed with a per-endpoint secret that is shown once, on creation
 * and on rotation.
 */
export function IntegrationsTab() {
  const t = useT();
  const load = useLoad(loadAll, []);
  const [form, setForm] = useState<{ existing?: WebhookEndpoint } | null>(null);
  const [secret, setSecret] = useState<{ title: string; value: string } | null>(null);

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const { endpoints, kinds } = load.data;
  const addBtn = <Button variant="primary" icon={<Plus size={ICON.md} aria-hidden />} onClick={() => setForm({})}>{t.t("Add endpoint")}</Button>;

  return (
    <>
      <Card
        title={t.t("Webhooks")}
        {...(endpoints.length ? { subtitle: t.plural(endpoints.length, { one: t.t("{{n}} endpoint"), other: t.t("{{n}} endpoints") }) } : {})}
        actions={
          <>
            <HelpPopover
              label={t.t("About webhooks")}
              body={t.t("Outbound HTTP callbacks for event / team / purchase lifecycle. Each delivery is signed with HMAC-SHA256; verify the X-Zv-Signature header against your endpoint's secret.")}
            />
            {endpoints.length > 0 && addBtn}
          </>
        }
      >
        {endpoints.length === 0 ? (
          <Empty title={t.t("No webhook endpoints yet. Add one to receive lifecycle events on a URL you control.")} action={addBtn} />
        ) : (
          <ul className="wh-eps">
            {endpoints.map((ep) => (
              <EndpointRow key={ep.id} ep={ep} onEdit={() => setForm({ existing: ep })} onSecret={(value) => setSecret({ title: t.t("New webhook secret"), value })} reload={load.reload} />
            ))}
          </ul>
        )}
      </Card>
      {form && (
        <WebhookForm
          kinds={kinds}
          existing={form.existing}
          onClose={() => setForm(null)}
          onSaved={(newSecret) => {
            setForm(null);
            load.reload();
            if (newSecret) setSecret({ title: t.t("Webhook created"), value: newSecret });
          }}
        />
      )}
      {secret && <SecretReveal title={secret.title} secret={secret.value} onClose={() => setSecret(null)} />}
    </>
  );
}

function EndpointRow({ ep, onEdit, onSecret, reload }: { ep: WebhookEndpoint; onEdit: () => void; onSecret: (s: string) => void; reload: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState<string | null>(null);
  const kinds = ep.event_kinds ?? [];

  const run = async (key: string, fn: () => Promise<void>) => {
    setBusy(key);
    try {
      await fn();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(null);
    }
  };

  // Offered only while the endpoint is enabled.
  const test = () =>
    void run("test", async () => {
      await testWebhook(ep.id);
      toast.ok(t.t("Test event sent. It should reach your endpoint within a few seconds."));
    });

  const rotate = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Rotate secret?"),
      message: t.t("The current secret stops working at once, and you see the new one only after rotating: deliveries fail verification until your verifier has it."),
      confirmLabel: t.t("Rotate secret"),
      danger: true,
    });
    if (!ok) return;
    void run("rotate", async () => {
      const r = await rotateSecret(ep.id);
      if (!r?.secret) throw new Error(t.t("Something went wrong"));
      onSecret(r.secret);
      reload();
    });
  };

  const toggle = () =>
    void run("toggle", async () => {
      await updateWebhook(ep.id, { active: !ep.active });
      toast.ok(ep.active ? t.t("Endpoint disabled. No more events will be sent to it.") : t.t("Endpoint enabled. Events will be sent to it again."));
      reload();
    });

  const remove = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Delete endpoint?"),
      message: t.t("This stops all future deliveries to this URL. Past deliveries are removed too. Cannot be undone."),
      confirmLabel: t.t("Delete endpoint"),
      danger: true,
    });
    if (!ok) return;
    void run("delete", async () => {
      await deleteWebhook(ep.id);
      toast.ok(t.t("Endpoint deleted."));
      reload();
    });
  };

  const locale = t.current();
  return (
    <li className="wh-ep">
      <div className="wh-ep__head">
        <strong className="wh-ep__name">{ep.name || t.t("(unnamed endpoint)")}</strong>
        {!ep.active && <Pill>{t.t("Disabled")}</Pill>}
      </div>
      <p className="wh-url">{ep.url}</p>
      {kinds.length ? (
        <div className="wh-kinds">{kinds.map((k) => <code key={k} className="wh-kind">{k}</code>)}</div>
      ) : (
        <p className="wh-muted wh-small">{t.t("(no events subscribed)")}</p>
      )}
      <p className="wh-muted wh-small">
        {t.t("Secret hint: ")}
        <code>{(ep.secret_hint || "") + "…"}</code>
        {" · "}
        <span title={fmtDateTime(ep.created_at, locale)}>{t.t("Created {{date}}", { date: relTime(ep.created_at, locale) })}</span>
      </p>
      <div className="wh-ep__acts">
        {ep.active && <Button size="sm" icon={<FastForward size={ICON.sm} aria-hidden />} title={t.t("Send test event")} loading={busy === "test"} onClick={test}>{t.t("Send test")}</Button>}
        <a className="btn sm wh-linkbtn" href={href({ name: "webhook-log", endpoint: ep.id })} title={t.t("Inspect this endpoint's delivery history")}>
          <ScrollText size={ICON.sm} aria-hidden />
          {t.t("Delivery log")}
        </a>
        <Button size="icon" variant="ghost" icon={<Pencil size={ICON.sm} aria-hidden />} aria-label={t.t("Edit")} title={t.t("Edit")} onClick={onEdit} />
        <MoreMenu size="sm"
          label={t.t("More")}
          items={[
            { label: t.t("Rotate secret"), icon: <KeyRound size={ICON.sm} aria-hidden />, title: t.t("Mint a new secret. The old one stops working immediately."), onClick: () => void rotate() },
            { label: ep.active ? t.t("Disable") : t.t("Enable"), icon: <Power size={ICON.sm} aria-hidden />, onClick: toggle },
            { separator: true },
            { label: t.t("Delete endpoint"), icon: <Trash2 size={ICON.sm} aria-hidden />, danger: true, onClick: () => void remove() },
          ]}
        />
      </div>
    </li>
  );
}

function WebhookForm({ kinds, existing, onClose, onSaved }: { kinds: string[]; existing?: WebhookEndpoint | undefined; onClose: () => void; onSaved: (secret?: string) => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const initial = useRef({ name: existing?.name ?? "", url: existing?.url ?? "", kinds: [...(existing?.event_kinds ?? [])].sort().join(",") });
  const [name, setName] = useState(initial.current.name);
  const [url, setUrl] = useState(initial.current.url);
  const [picked, setPicked] = useState<Set<string>>(() => new Set(existing?.event_kinds ?? []));
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const asking = useRef(false);

  const dirty = name !== initial.current.name || url !== initial.current.url || [...picked].sort().join(",") !== initial.current.kinds;
  const close = async () => {
    if (asking.current) return;
    if (dirty) {
      asking.current = true;
      const ok = await dialogs.confirm({
        title: existing ? t.t("Edit webhook") : t.t("Add webhook"),
        message: existing ? t.t("Discard unsaved changes to this webhook?") : t.t("Discard this webhook?"),
        confirmLabel: t.t("Discard"),
        danger: true,
      });
      asking.current = false;
      if (!ok) return;
    }
    onClose();
  };

  const submit = async () => {
    // In the order the catalogue lists them, as the classic form sent them.
    const eventKinds = kinds.filter((k) => picked.has(k)).concat([...picked].filter((k) => !kinds.includes(k)));
    if (eventKinds.length === 0) return setErr(t.t("Select at least one event kind."));
    setErr("");
    setBusy(true);
    try {
      if (existing) {
        await updateWebhook(existing.id, { name, url, event_kinds: eventKinds });
        toast.ok(t.t("Webhook updated."));
        onSaved();
      } else {
        const r = await createWebhook({ name, url, event_kinds: eventKinds });
        onSaved(r?.secret);
      }
    } catch (e) {
      // The server's refusals of the URL (internal/admin/webhooks.go) are in the catalogue.
      setErr(errorText(e));
      setBusy(false);
    }
  };

  const toggle = (k: string) => setPicked((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });

  return (
    <Dialog
      title={existing ? t.t("Edit webhook") : t.t("Add webhook")}
      onClose={() => void close()}
      onSubmit={() => void submit()}
      wide
      footer={
        <>
          <Button onClick={() => void close()}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{existing ? t.t("Save") : t.t("Create")}</Button>
        </>
      }
    >
      <Field label={t.t("Name")}>
        <input type="text" placeholder={t.t("e.g. prod-slack")} value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label={t.t("Endpoint URL")} hint={t.t("Must be an https address reachable from the internet.")}>
        <input type="url" required autoFocus placeholder="https://example.com/webhook" value={url} onChange={(e) => setUrl(e.target.value)} />
      </Field>
      <fieldset className="wh-fieldset">
        <legend>{t.t("Subscribed events")}</legend>
        <div className="wh-checks">
          {kinds.map((k) => (
            <label key={k} className="wh-check">
              <input type="checkbox" checked={picked.has(k)} onChange={() => toggle(k)} />
              <code>{k}</code>
            </label>
          ))}
        </div>
      </fieldset>
      {err && <p className="wh-err" role="alert">{err}</p>}
    </Dialog>
  );
}

function SecretReveal({ title, secret, onClose }: { title: string; secret: string; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      toast.ok(t.t("Secret copied"));
    } catch {
      toast.err(t.t("Copy failed"));
    }
  };
  return (
    <Dialog
      title={title}
      onClose={onClose}
      footer={
        <>
          <Button icon={<Copy size={ICON.md} aria-hidden />} onClick={() => void copy()}>{t.t("Copy")}</Button>
          <Button variant="primary" onClick={onClose}>{t.t("I've saved it")}</Button>
        </>
      }
    >
      <p className="wh-warn">{t.t("This is the only time the secret will be shown. Store it somewhere safe.")}</p>
      <pre className="wh-secret">{secret}</pre>
      <p className="wh-muted wh-small">{t.t('Configure your verifier with this value. Compute HMAC-SHA256 over the raw request body and compare to the X-Zv-Signature header (after stripping the "sha256=" prefix).')}</p>
      <p className="wh-muted wh-small">{t.t("A delivery can arrive more than once, after a retry or a replay. Skip any whose X-Zv-Delivery-Id header you have already handled: there is no timestamp header, so the delivery id is how you spot a repeat.")}</p>
    </Dialog>
  );
}
