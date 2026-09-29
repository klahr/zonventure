import { useRef, useState } from "react";
import { Check, Compass, Loader2, Upload, X } from "lucide-react";
import { get, post, request } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { href } from "../../router.ts";
import { Button, Callout, Card, ErrorBox, Field, HelpPopover, ICON, Loading, Row, errorText, useDialogs, useLoad, useToast } from "../../ui/index.ts";
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
}
interface Org {
  id: number;
  name: string;
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
      {mode.target && !mode.pinned_by_env ? <ActiveRedirect mode={mode} onChange={load.reload} /> : <MigrateOut orgs={orgs} mode={mode} maint={maint} onReset={load.reload} />}
      <ImportBundle />
    </>
  );
}

function ActiveRedirect({ mode, onChange }: { mode: RedirectMode; onChange: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [busy, setBusy] = useState(false);
  const disable = async () => {
    if (!(await dialogs.confirm({ title: t.t("Disable redirect mode"), message: t.t("Disable redirect mode? Pages on this instance will resume serving normally."), confirmLabel: t.t("Disable redirect mode"), danger: true }))) return;
    setBusy(true);
    try {
      await post("/api/admin/migrate/redirect-mode", { target: "" });
      toast.ok(t.t("Redirect mode disabled."));
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

function MigrateOut({ orgs, mode, maint, onReset }: { orgs: Org[]; mode: RedirectMode; maint: Maintenance; onReset: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [orgID, setOrgID] = useState(orgs[0] ? String(orgs[0].id) : "");
  const [target, setTarget] = useState(mode.target || "");
  const [running, setRunning] = useState<{ orgName: string; target: string } | null>(null);
  const [steps, setSteps] = useState<Record<StepID, StepState>>({ pause: "idle", export: "idle", upload: "idle", redirect: "idle" });
  const [log, setLog] = useState<{ err: boolean; text: string }>({ err: false, text: "" });
  const [finished, setFinished] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const aborted = useRef(false);
  const blocked = mode.pinned_by_env ? t.t("ZV_REDIRECT_TARGET is pinned in the environment file. Unset it in .env to use the wizard.") : "";
  const step = (id: StepID, s: StepState) => setSteps((x) => ({ ...x, [id]: s }));

  const start = async () => {
    const dest = target.trim();
    if (!orgID) return toast.err(t.t("Pick a source org."));
    if (!dest) return toast.err(t.t("Enter the destination URL."));
    if (!/^https?:\/\//.test(dest)) return toast.err(t.t("Destination must start with http:// or https://"));
    const orgName = orgs.find((o) => String(o.id) === orgID)?.name || `#${orgID}`;
    const ok = await dialogs.confirm({
      title: t.t("Start migration"),
      message: t.t('Migrate "{{name}}" to {{target}}? You can stop before the redirect step. Once redirect flips, this instance will 301 every page to the destination.', { name: orgName, target: dest }),
      confirmLabel: t.t("Start migration"),
      danger: true,
    });
    if (!ok) return;
    aborted.current = false;
    setRunning({ orgName, target: dest });

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
    toast.ok(t.t("Migration cancelled."));
    onReset();
    setRunning(null);
    setCancelling(false);
    setSteps({ pause: "idle", export: "idle", upload: "idle", redirect: "idle" });
    setLog({ err: false, text: "" });
  };

  const flip = async () => {
    if (!running || aborted.current) return;
    const ok = await dialogs.confirm({
      title: t.t("Activate redirect to destination"),
      message: t.t("Flip redirect mode now? Every page on this instance will 301 to {{target}} from the moment you confirm.", { target: running.target }),
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
          <Field label={t.t("Source org")}>
            <select value={orgID} onChange={(e) => setOrgID(e.target.value)}>
              {orgs.map((o) => <option key={o.id} value={o.id}>{`${o.name} (#${o.id})`}</option>)}
            </select>
          </Field>
          <Field label={t.t("Destination URL")}>
            <input type="url" placeholder="https://app.zonventure.com" value={target} onChange={(e) => setTarget(e.target.value)} />
          </Field>
          <Row>
            <Button variant="primary" icon={<Upload size={ICON.md} aria-hidden />} onClick={() => void start()}>{t.t("Start migration")}</Button>
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
              <Row><Button variant="primary" icon={<Compass size={ICON.md} aria-hidden />} onClick={() => void flip()}>{t.t("I've uploaded the bundle — flip redirect now")}</Button></Row>
            </div>
          )}
          {finished && (
            <div className="mg-box">
              <p>{t.t("Done. Suggested follow-ups:")}</p>
              <ul>
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
        text: t.t("Imported as org #{{id}}: {{adv}} adventures, {{ev}} events, {{tm}} teams, {{sl}} slots, {{pu}} ticket purchases.", {
          id: s.org_id, adv: s.adventures, ev: s.events, tm: s.teams, sl: s.slots, pu: s.ticket_purchases,
        }),
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
    </Card>
  );
}
