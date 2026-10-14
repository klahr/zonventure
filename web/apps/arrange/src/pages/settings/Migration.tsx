import { useRef, useState } from "react";
import { Check, Compass, Loader2, Upload, X } from "lucide-react";
import { get, post, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Card, ErrorBox, Field, HelpPopover, ICON, Loading, Row, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";
import { SetPasswordDialog } from "../admin/OrgDialogs.tsx";
import "./migration-steps.css";

/** RedirectModeStatus and MaintenanceModeStatus, internal/admin/migrate.go. */
interface RedirectMode {
  target: string;
  pinned_by_env: boolean;
}
interface Maintenance {
  on: boolean;
  pinned_by_env: boolean;
}
/** OrgImportSummary, internal/admin/portability_org.go. */
interface ImportSummary {
  org_id: number;
  adventures: number;
  events: number;
  teams: number;
  slots: number;
  ticket_purchases: number;
  invoice_purchases?: number;
  flags?: ("invoice_billing" | "party_adventures" | "verified_creator")[];
  no_email?: { id: number; username: string }[];
}
interface Org {
  id: number;
  name: string;
  events_total: number;
  adventures_total: number;
}

interface Snapshot {
  orgs: Org[];
  mode: RedirectMode;
  maint: Maintenance;
}

async function loadSnapshot(signal: AbortSignal): Promise<Snapshot> {
  const [orgs, mode, maint] = await Promise.all([
    get<{ organizations: Org[] | null }>("/api/admin/organizations", { signal }),
    get<RedirectMode>("/api/admin/migrate/redirect-mode", { signal }),
    get<Maintenance>("/api/admin/migrate/maintenance", { signal }),
  ]);
  return { orgs: orgs.organizations ?? [], mode: mode ?? { target: "", pinned_by_env: false }, maint: maint ?? { on: false, pinned_by_env: false } };
}

/**
 * Move an org to another zv installation (freeze, download the bundle,
 * upload it there, redirect here), and take in a bundle from elsewhere.
 */
export function MigrationTab() {
  const t = useT();
  const load = useLoad(loadSnapshot, []);
  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const { orgs, mode, maint } = load.data;
  return (
    <>
      {mode.target && !mode.pinned_by_env ? <ActiveRedirect mode={mode} maint={maint} onChange={load.reload} /> : <MigrateOut orgs={orgs} mode={mode} maint={maint} onReset={load.reload} />}
      <ImportBundle />
    </>
  );
}

function ActiveRedirect({ mode, maint, onChange }: { mode: RedirectMode; maint: Maintenance; onChange: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState(false);
  const disable = async () => {
    if (!(await dialogs.confirm({ title: t.t("Disable redirect mode"), message: t.t("Disable redirect mode? Pages on this instance will resume serving normally."), confirmLabel: t.t("Disable redirect mode"), danger: true }))) return;
    setBusy(true);
    try {
      await post("/api/admin/migrate/redirect-mode", { target: "" });
      // Maintenance stays as it was: say so, or it looks like the instance is back.
      toast.ok(maint.on ? t.t("Redirect mode disabled. Maintenance mode is still on: turn it off under System when this instance should save again.") : t.t("Redirect mode disabled."));
      onChange();
    } catch (e) {
      toast.err(errorText(e) || t.t("Could not disable redirect mode."));
      setBusy(false);
    }
  };
  return (
    <Card title={t.t("Redirect mode is ON")}>
      <p style={{ margin: 0, color: "var(--dz-muted)" }}>{t.t("Every non-allowlisted page on this instance 301s to:")}</p>
      <p style={{ margin: 0, fontSize: "1.05rem" }}><strong>{mode.target}</strong></p>
      <Row><Button variant="ghost" loading={busy} onClick={() => void disable()}>{t.t("Disable redirect mode")}</Button></Row>
    </Card>
  );
}

type StepID = "pause" | "export" | "upload" | "redirect";
type StepState = "idle" | "running" | "done" | "error";

/** The migration this browser started, kept so a reload can pick it up again. */
interface Started {
  orgID: string;
  orgName: string;
  target: string;
}
const STARTED_KEY = "zv_migration_started";
const IDLE: Record<StepID, StepState> = { pause: "idle", export: "idle", upload: "idle", redirect: "idle" };
const AWAITING_UPLOAD: Record<StepID, StepState> = { pause: "done", export: "done", upload: "running", redirect: "idle" };

function loadStarted(): Started | null {
  try {
    const v = JSON.parse(localStorage.getItem(STARTED_KEY) ?? "null") as Started | null;
    return v && v.orgID && v.target ? v : null;
  } catch {
    return null;
  }
}

function saveStarted(v: Started | null) {
  try {
    if (v) localStorage.setItem(STARTED_KEY, JSON.stringify(v));
    else localStorage.removeItem(STARTED_KEY);
  } catch { /* private mode */ }
}

function MigrateOut({ orgs, mode, maint, onReset }: { orgs: Org[]; mode: RedirectMode; maint: Maintenance; onReset: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  // Maintenance on (and not pinned) after a reload means a migration was
  // left between the download and the redirect: pick it up there.
  const halfDone = maint.on && !maint.pinned_by_env;
  const [resumed] = useState(() => (halfDone ? loadStarted() : null));
  const [orgID, setOrgID] = useState(resumed?.orgID ?? (orgs[0] ? String(orgs[0].id) : ""));
  const [target, setTarget] = useState(resumed?.target ?? (mode.target || ""));
  const [running, setRunning] = useState<Started | null>(resumed);
  const [steps, setSteps] = useState<Record<StepID, StepState>>(resumed ? AWAITING_UPLOAD : IDLE);
  const [log, setLog] = useState<{ err: boolean; text: string }>(
    resumed ? { err: false, text: t.t("Maintenance mode is still on from this migration. Carry on below, or cancel it.") } : { err: false, text: "" },
  );
  const [finished, setFinished] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const aborted = useRef(false);
  const blocked = mode.pinned_by_env ? t.t("ZV_REDIRECT_TARGET is pinned in the environment file. Unset it in .env to use the wizard.") : "";
  const step = (id: StepID, s: StepState) => setSteps((x) => ({ ...x, [id]: s }));

  /** The form's org and destination, checked; null (after saying why) when they won't do. */
  const chosen = (): Started | null => {
    const dest = target.trim();
    if (!orgID) { toast.err(t.t("Pick a source org.")); return null; }
    if (!dest) { toast.err(t.t("Enter the destination URL.")); return null; }
    if (!/^https?:\/\//.test(dest)) { toast.err(t.t("Destination must start with http:// or https://")); return null; }
    return { orgID, orgName: orgs.find((o) => String(o.id) === orgID)?.name || `#${orgID}`, target: dest };
  };

  // Maintenance is already on and the bundle was downloaded before: go
  // straight to the upload step instead of building it again.
  const resume = () => {
    const c = chosen();
    if (!c) return;
    aborted.current = false;
    saveStarted(c);
    setRunning(c);
    setSteps(AWAITING_UPLOAD);
    setLog({ err: false, text: "" });
  };

  const start = async () => {
    const c = chosen();
    if (!c) return;
    const { orgName, target: dest } = c;
    const ok = await dialogs.confirm({
      title: t.t("Start migration"),
      message: t.t('Migrate "{{name}}" to {{target}}? You can stop before the redirect step. Once redirect flips, this instance will 301 every page to the destination.', { name: orgName, target: dest }),
      confirmLabel: t.t("Start migration"),
      danger: true,
    });
    if (!ok) return;
    aborted.current = false;
    setRunning(c);

    // 1. Freeze writes. An env-pinned maintenance mode is already on
    // (and the toggle would 409), so just note it.
    step("pause", "running");
    if (maint.pinned_by_env) {
      setLog({ err: false, text: t.t("Maintenance mode pinned by env — leaving as-is.") });
    } else {
      try {
        await post("/api/admin/migrate/maintenance", { on: true });
      } catch (e) {
        step("pause", "error");
        setLog({ err: true, text: t.t("Could not enable maintenance: {{err}}", { err: errorText(e) }) });
        return;
      }
    }
    step("pause", "done");
    if (aborted.current) return;
    saveStarted(c);

    // 2. The download. A click-driven download reports nothing back,
    // so the step counts as done once it starts; the operator confirms
    // by moving on to the upload.
    step("export", "running");
    const a = document.createElement("a");
    a.href = `/api/admin/migrate/export?org_id=${encodeURIComponent(orgID)}`;
    a.rel = "noopener";
    document.body.append(a);
    a.click();
    a.remove();
    step("export", "done");
    setLog({ err: false, text: "" });
    // 3. The operator uploads it on the destination by hand.
    step("upload", "running");
  };

  const cancel = async () => {
    const ok = await dialogs.confirm({
      title: t.t("Cancel"),
      message: t.t("Cancel the migration? Maintenance mode will be turned back off; any bundle already downloaded is your responsibility to discard."),
      confirmLabel: t.t("Cancel the migration"),
      cancelLabel: t.t("Keep going"),
      danger: true,
    });
    if (!ok) return;
    aborted.current = true;
    setCancelling(true);
    setLog({ err: false, text: t.t("Disabling maintenance mode…") });
    if (!maint.pinned_by_env) await post("/api/admin/migrate/maintenance", { on: false }).catch(() => {});
    saveStarted(null);
    toast.ok(t.t("Migration cancelled."));
    onReset();
    setRunning(null);
    setCancelling(false);
    setSteps(IDLE);
    setLog({ err: false, text: "" });
  };

  const flip = async () => {
    if (!running || aborted.current) return;
    // Redirect mode is instance-wide: every other org with content loses its pages too.
    const others = orgs.filter((o) => String(o.id) !== running.orgID && (o.events_total > 0 || o.adventures_total > 0)).map((o) => o.name);
    const warning = others.length ? " " + t.t("Other organisations on this instance have data and will be redirected too: {{names}}.", { names: others.join(", ") }) : "";
    const ok = await dialogs.confirm({
      title: t.t("Activate redirect to destination"),
      message: t.t("Flip redirect mode now? Every page on this instance except this console will 301 to {{target}} from the moment you confirm.", { target: running.target }) + warning,
      confirmLabel: t.t("Activate redirect to destination"),
      danger: true,
    });
    if (!ok) return;
    step("upload", "done");
    step("redirect", "running");
    try {
      await post("/api/admin/migrate/redirect-mode", { target: running.target });
    } catch (e) {
      step("redirect", "error");
      setLog({ err: true, text: t.t("Could not enable redirect: {{err}}", { err: errorText(e) }) });
      return;
    }
    step("redirect", "done");
    saveStarted(null);
    setLog({ err: false, text: t.t("Migration complete. This instance now redirects to {{target}}.", { target: running.target }) });
    setFinished(true);
  };

  const labels: Record<StepID, string> = {
    pause: t.t("Pause writes (maintenance mode)"),
    export: t.t("Build and download bundle"),
    upload: t.t("Upload bundle on destination"),
    redirect: t.t("Activate redirect to destination"),
  };
  const icon = (s: StepState) =>
    s === "done" ? <Check size={ICON.md} aria-hidden /> : s === "error" ? <X size={ICON.md} aria-hidden /> : s === "running" ? <Loader2 size={ICON.md} className="mg-spin" aria-hidden /> : <span aria-hidden>·</span>;
  const stateLabel = (s: StepState) => (s === "done" ? t.t("Done") : s === "error" ? t.t("Failed") : s === "running" ? t.t("In progress") : t.t("Not started"));
  const destURL = running ? running.target.replace(/\/$/, "") + "/arrange#settings/migration" : "";

  return (
    <Card
      title={
        <>
          {t.t("Migrate an org to another instance")}{" "}
          <HelpPopover label={t.t("About migration")} body={t.t("Move an org from this instance to another zv installation. The wizard pauses writes, downloads a bundle for you to upload on the destination, then redirects this instance there so existing QR codes and magic links keep working.")} />
        </>
      }
      subtitle={t.t("One operation, run end to end. The wizard handles the freeze, export, and redirect — you upload the zip on the destination in between.")}
    >
      {blocked ? (
        <Callout variant="info">{blocked}</Callout>
      ) : !running && orgs.length === 0 ? (
        <p className="mg-log">{t.t("No orgs to export")}</p>
      ) : !running ? (
        <>
          {maint.pinned_by_env && (
            <Callout variant="info">{t.t("Maintenance mode is on: ZV_MAINTENANCE_MODE is set in the environment. Unset it and restart the app to turn it off.")}</Callout>
          )}
          {halfDone && (
            <Callout variant="warning">
              {t.t("Maintenance mode is on, so a migration may be half done. If you already downloaded its bundle, pick the same org and destination and continue; otherwise start over, or cancel the migration to turn maintenance off.")}
            </Callout>
          )}
          <Field label={t.t("Source org")}>
            <select value={orgID} onChange={(e) => setOrgID(e.target.value)}>
              {orgs.map((o) => <option key={o.id} value={o.id}>{`${o.name} (#${o.id})`}</option>)}
            </select>
          </Field>
          <Field label={t.t("Destination URL")}>
            <input type="url" placeholder="https://zonventure.example.com" value={target} onChange={(e) => setTarget(e.target.value)} />
          </Field>
          <Row>
            <Button variant="primary" icon={<Upload size={ICON.md} aria-hidden />} onClick={() => void start()}>{t.t("Start migration")}</Button>
            {halfDone && <Button onClick={resume}>{t.t("Continue with the downloaded bundle")}</Button>}
            {halfDone && <Button variant="ghost" disabled={cancelling} onClick={() => void cancel()}>{t.t("Cancel the migration")}</Button>}
          </Row>
        </>
      ) : (
        <>
          <ol className="mg-steps">
            {(Object.keys(labels) as StepID[]).map((id) => (
              <li key={id} className={`mg-step is-${steps[id]}`}>
                <span className="mg-step__icon">{icon(steps[id])}</span>
                <span>{labels[id]}</span>
                <span className="cs-sr">{stateLabel(steps[id])}</span>
              </li>
            ))}
          </ol>
          {log.text && <p className={"mg-log" + (log.err ? " is-err" : "")} role="status">{log.text}</p>}
          {!finished && (
            <Row><Button size="sm" variant="ghost" disabled={cancelling || steps.redirect !== "idle"} onClick={() => void cancel()}>{t.t("Cancel")}</Button></Row>
          )}
          {steps.upload === "running" && (
            <div className="mg-box">
              <p>{t.t("Bundle saved to your downloads folder. Now:")}</p>
              <ol>
                <li>{t.t("Open ")}<a href={destURL} target="_blank" rel="noopener">{destURL}</a></li>
                <li>{t.t("Sign in there with your admin account.")}</li>
                <li>{t.t('Pick the bundle file and click "Import bundle".')}</li>
              </ol>
              <p>{t.t("No file in your downloads?")} <a href={`/api/admin/migrate/export?org_id=${encodeURIComponent(orgID)}`} rel="noopener">{t.t("Download the bundle again")}</a></p>
              <Row><Button variant="primary" icon={<Compass size={ICON.md} aria-hidden />} onClick={() => void flip()}>{t.t("I've uploaded the bundle — flip redirect now")}</Button></Row>
            </div>
          )}
          {finished && (
            <div className="mg-box">
              <p>{t.t("Done. Suggested follow-ups:")}</p>
              <ul>
                <li>{t.t("Remove this instance's webhook endpoint in the Stripe dashboard, so Stripe events don't fire twice.")}</li>
                <li>{t.t("Take a final pg_dump of this instance before tearing it down.")}</li>
                <li>{t.t("Once you've confirmed the destination is serving traffic, this instance can be retired.")}</li>
              </ul>
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function ImportBundle() {
  const t = useT();
  const toast = useToast();
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<{ err: boolean; text: string; summary?: ImportSummary } | null>(null);
  const [setting, setSetting] = useState<{ id: number; username: string } | null>(null);
  const [passwordSet, setPasswordSet] = useState<number[]>([]);

  const run = async () => {
    if (!file) return toast.err(t.t("Pick a bundle zip first"));
    setBusy(true);
    setStatus({ err: false, text: t.t("Uploading and replaying — this can take a moment.") });
    const form = new FormData();
    form.append("bundle", file);
    try {
      const s = await request<ImportSummary>("/api/admin/migrate/import", { method: "POST", form });
      setStatus({
        err: false,
        summary: s,
        text: t.t("Imported as org #{{id}}: {{counts}}.", {
          id: s.org_id,
          counts: [
            t.plural(s.adventures, { one: t.t("{{n}} adventure"), other: t.t("{{n}} adventures") }),
            t.plural(s.events, { one: t.t("{{n}} event"), other: t.t("{{n}} events") }),
            t.plural(s.teams, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") }),
            t.plural(s.slots, { one: t.t("{{n}} slot"), other: t.t("{{n}} slots") }),
            t.plural(s.ticket_purchases, { one: t.t("{{n}} ticket purchase"), other: t.t("{{n}} ticket purchases") }),
          ].join(", "),
        }) + (s.invoice_purchases ? " " + t.plural(s.invoice_purchases, { one: t.t("{{n}} unbilled invoice purchase came along."), other: t.t("{{n}} unbilled invoice purchases came along.") }) : ""),
      });
      toast.ok(t.t("Bundle imported."));
    } catch (e) {
      setStatus({ err: true, text: t.t("Import failed: {{err}}", { err: errorText(e) }) });
      toast.err(t.t("Import failed."));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card
      title={
        <>
          {t.t("Import a bundle into this instance")}{" "}
          <HelpPopover label={t.t("About importing")} body={t.t("Pick the zip downloaded from another zv instance's migration wizard. A fresh org is minted here with all records replayed under new IDs. Slug collisions reject with 409 — rename on the source side and re-export.")} />
        </>
      }
      subtitle={t.t("Pick the zip downloaded from another zv instance's migration wizard.")}
    >
      <Row>
        <Field label={t.t("Bundle")}>
          <input type="file" accept=".zip,application/zip" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
        </Field>
        <Button icon={<Upload size={ICON.md} aria-hidden />} loading={busy} disabled={!file} onClick={() => void run()}>{t.t("Import bundle")}</Button>
      </Row>
      {status && (
        <p className={"mg-log" + (status.err ? " is-err" : "")} role="status">
          {status.text}
          {status.summary && <> <a href={href({ name: "organization", id: status.summary.org_id })}>{t.t("Open Organizations →")}</a></>}
        </p>
      )}
      {!!status?.summary?.flags?.length && (
        <p className="mg-log" role="status">
          {t.t("Turned on as in the source: {{flags}}. Check them on the organization page.", {
            flags: status.summary.flags.map((f) => ({ invoice_billing: t.t("Invoice billing"), party_adventures: t.t("Public party creator"), verified_creator: t.t("Verified creator") })[f]).join(", "),
          })}
        </p>
      )}
      {!!status?.summary?.no_email?.length && (
        <Callout variant="warning">
          <p>{t.t("These members have no email and no password, so they can't sign in or reset it themselves. Set a password for each and pass it on:")}</p>
          <ul>
            {status.summary.no_email.map((m) => (
              <li key={m.id}>
                <strong>{m.username}</strong>{" "}
                {passwordSet.includes(m.id)
                  ? t.t("Password set")
                  : <Button size="sm" variant="ghost" onClick={() => setSetting(m)}>{t.t("Set password")}</Button>}
              </li>
            ))}
          </ul>
        </Callout>
      )}
      {setting && <SetPasswordDialog member={setting} onClose={() => setSetting(null)} onDone={() => { setPasswordSet((x) => [...x, setting.id]); setSetting(null); }} />}
    </Card>
  );
}
