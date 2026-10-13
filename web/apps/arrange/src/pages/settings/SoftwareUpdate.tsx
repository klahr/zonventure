import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { get, post } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, Facts, HelpPopover, ICON, Loading, Pill, Row, errorText, relTime, useLoad, type Tone } from "../../ui/index.ts";

/** updates.Status, internal/updates/service.go. */
interface UpdateStatus {
  current?: string;
  current_raw?: string;
  is_dev_build?: boolean;
  latest?: string;
  latest_date?: string;
  update_available?: boolean;
  sections?: { version: string; date?: string; notes_html?: string }[];
  checked_at?: string;
  fetch_error?: string;
  disabled?: boolean;
}

const muted = { margin: 0, color: "var(--dz-muted)", fontSize: "var(--dz-text-sm)" };

/**
 * Whether a newer release exists, what changed since the running one,
 * and how to upgrade. Upgrading stays a shell job; this page says when
 * there is one to do.
 */
export function SoftwareUpdateTab() {
  const t = useT();
  const load = useLoad((signal) => get<UpdateStatus>("/api/admin/system-update", { signal }), []);
  const [checking, setChecking] = useState(false);
  const [checkErr, setCheckErr] = useState("");

  const check = async () => {
    setChecking(true);
    setCheckErr("");
    try {
      load.set((await post<UpdateStatus>("/api/admin/system-update/check")) ?? {});
    } catch (e) {
      setCheckErr(errorText(e));
    } finally {
      setChecking(false);
    }
  };

  if (load.error && !load.data) return <ErrorBox message={load.error} retry={load.reload} retryLabel={t.t("Retry")} />;
  if (!load.data) return <Loading label={t.t("Loading…")} />;
  const s = load.data;
  // No checked_at means the snapshot itself never arrived.
  const couldNotCheck = (!!s.fetch_error && !s.latest) || !s.checked_at;
  const [tone, label]: [Tone, string] = s.disabled
    ? ["neutral", t.t("Off")]
    : s.is_dev_build
    ? ["warn", t.t("Development build")]
    : s.update_available
      ? ["accent", t.t("Update available")]
      : couldNotCheck
        ? ["warn", t.t("Could not check")]
        : ["good", t.t("Up to date")];
  const monoBig = { fontFamily: "var(--dz-font-mono)", fontSize: "1.1rem" };

  const upgrade = !s.is_dev_build && !!s.update_available;
  const howTo = t.t("Fetches the newest release tag, checks it out, and rebuilds the Docker image. Migrations run automatically on startup. Back up Postgres first if you're crossing a major version.");

  return (
    <>
      <Card
        title={<>{t.t("Software update")} <Pill tone={tone}>{label}</Pill></>}
        subtitle={t.t("Checks once a day whether a newer version of Zonventure is available. Click Check now to look immediately.")}
      >
        <Facts
          items={[
            { label: t.t("Current"), value: <span style={monoBig}>{s.current_raw || t.t("unknown")}</span> },
            { label: t.t("Latest"), value: <span style={monoBig}>{s.latest ? s.latest + (s.latest_date ? `  (${s.latest_date})` : "") : t.t("unknown")}</span> },
          ]}
        />
        {s.disabled && <p style={muted}>{t.t("Update checks are off: ZV_CHANGELOG_URL is set empty, so nothing is fetched.")}</p>}
        {s.is_dev_build && <p style={muted}>{t.t("This binary reports a version past the last tagged release. Update prompts are suppressed because we can't tell whether the upstream changelog covers commits this build already includes.")}</p>}
        <Row gap="sm">
          {!s.disabled && (
            <>
              <Button size="sm" icon={<RefreshCw size={ICON.sm} aria-hidden />} loading={checking} onClick={() => void check()}>{checking ? t.t("Checking…") : t.t("Check now")}</Button>
              <span style={muted} title={s.checked_at}>{s.checked_at ? t.t("Checked {{when}}", { when: relTime(s.checked_at, t.current()) }) : t.t("Not checked yet.")}</span>
            </>
          )}
          {!upgrade && <HelpPopover label={t.t("How to upgrade")} body={"make upgrade — " + howTo} />}
          {checkErr && <span style={{ ...muted, color: "var(--dz-danger)" }} role="alert">{checkErr}</span>}
        </Row>
        {s.fetch_error && <p style={muted}>{t.t("Last error: {{err}}", { err: s.fetch_error })}</p>}
      </Card>
      {upgrade && s.sections?.length ? (
        <Card title={t.t("Changes since {{v}}", { v: s.current || s.current_raw || "" })}>
          {s.sections.map((sec) => (
            <section key={sec.version} style={{ display: "flex", flexDirection: "column", gap: "var(--dz-space-1)" }}>
              <Row gap="sm">
                <strong style={{ fontFamily: "var(--dz-font-mono)", fontSize: "1.05rem" }}>{sec.version}</strong>
                {sec.date && <span style={muted}>{sec.date}</span>}
              </Row>
              {/* Rendered server-side from the release changelog. */}
              <div className="md-preview" dangerouslySetInnerHTML={{ __html: sec.notes_html || "" }} />
            </section>
          ))}
        </Card>
      ) : null}
      {upgrade && (
        <Card title={t.t("How to upgrade")}>
          <pre style={{ margin: 0, fontFamily: "var(--dz-font-mono)", background: "var(--dz-surface-2)", padding: "var(--dz-space-2) var(--dz-space-3)", borderRadius: "var(--dz-r-2)", overflowX: "auto" }}>make upgrade</pre>
          <p style={muted}>{howTo}</p>
        </Card>
      )}
    </>
  );
}
