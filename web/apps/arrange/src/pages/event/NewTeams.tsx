import { useState } from "react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, errorText, Field, useDialogs, useToast } from "../../ui/index.ts";
import * as api from "./api.ts";
import type { BulkResult, RosterTeam, Team } from "./api.ts";

type Mode = "single" | "bulk" | "roster" | "import";

const splitNames = (s: string) => s.split(/[\n,;]+/).map((x) => x.trim()).filter(Boolean);

/**
 * One row per team: the first cell is its name, the rest its players.
 * Comma, semicolon, tab and colon all separate, so a spreadsheet paste
 * and "Team: alice, bob" both parse.
 */
export function parseRoster(text: string): RosterTeam[] {
  const out: RosterTeam[] = [];
  for (const raw of text.split(/\r?\n/)) {
    const parts = raw.trim().split(/[\t,;:]+/).map((s) => s.trim()).filter(Boolean);
    if (parts.length === 0) continue;
    const [name, ...member_names] = parts;
    out.push({ name: name!, member_names });
  }
  return out;
}

/**
 * Add teams four ways: one team with its players, many placeholder
 * teams at once, a pasted roster, or an uploaded file (CSV, Excel,
 * OpenDocument, IOF XML) the server reads and previews. Rosters and
 * files land through the same /teams/bulk writer.
 */
export function NewTeamsDialog({ eventId, teams, onClose, onDone }: { eventId: number; teams: Team[]; onClose: () => void; onDone: () => void }) {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const existing = new Set(teams.map((tm) => tm.name));
  const [mode, setMode] = useState<Mode>("single");
  const [busy, setBusy] = useState(false);
  // single
  const [name, setName] = useState("");
  const [names, setNames] = useState("");
  const [email, setEmail] = useState("");
  const [touched, setTouched] = useState({ name: false, names: false });
  // bulk
  const [count, setCount] = useState("5");
  const [per, setPer] = useState("3");
  const [teamPrefix, setTeamPrefix] = useState("Team");
  const [playerPrefix, setPlayerPrefix] = useState("Player");
  // roster
  const [roster, setRoster] = useState("");
  // import
  const [imported, setImported] = useState<{ format: string; teams: RosterTeam[] } | null>(null);
  const [importMsg, setImportMsg] = useState<{ text: string; err?: boolean } | null>(null);

  const nameErr = !name.trim() ? (touched.name ? t.t("Team name required.") : null) : existing.has(name.trim()) ? t.t('A team named "{{name}}" already exists in this event.', { name: name.trim() }) : null;
  const namesErr = touched.names && splitNames(names).length === 0 ? t.t("Add at least one member — each line becomes a player slot.") : null;
  const parsed = parseRoster(roster);
  const offender = parsed.find((tm) => tm.member_names.length === 0);

  const dirty = !!(name.trim() || names.trim() || roster.trim() || imported?.teams.length);
  const close = async () => {
    if (!dirty || (await dialogs.confirm({ title: t.t("Discard the team(s) you were adding?"), confirmLabel: t.t("Discard"), danger: true }))) onClose();
  };

  const readFile = async (f: File | undefined) => {
    setImported(null);
    if (!f) return setImportMsg(null);
    setImportMsg({ text: t.t("Reading file…") });
    try {
      const body = await api.importTeams(eventId, f);
      const list = body.teams ?? [];
      const players = list.reduce((n, tm) => n + (tm.member_names ?? []).length, 0);
      const head = list.slice(0, 3).map((tm) => tm.name).join(", ");
      const names = list.length > 3 ? t.t("{{head}}, +{{n}} more", { head, n: list.length - 3 }) : head;
      setImported({ format: body.format, teams: list });
      setImportMsg({ text: t.t("Recognised as {{fmt}}", { fmt: body.format }) + ": " + sizes(list.length, players) + " — " + names });
    } catch (e) {
      setImportMsg({ text: e instanceof ApiError ? e.message : errorText(e), err: true });
    }
  };

  const sizes = (teams: number, slots: number) =>
    t.plural(teams, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") }) + " · " + t.plural(slots, { one: t.t("{{n}} player slot"), other: t.t("{{n}} player slots") });
  const createdText = (n: number) => t.plural(n, { one: t.t("Created {{n}} team"), other: t.t("Created {{n}} teams") });
  const importedText = (n: number) => t.plural(n, { one: t.t("Imported {{n}} team"), other: t.t("Imported {{n}} teams") });

  const reportBulk = (r: BulkResult, ok: (n: number) => string, partKey: string) => {
    const created = r.created || 0, failed = r.failed || 0;
    if (failed === 0) toast.ok(ok(created));
    else toast.err(t.t(partKey, { c: created, f: failed, err: r.results?.find((x) => x.error)?.error || "" }));
  };

  const submit = async () => {
    setBusy(true);
    try {
      if (mode === "import") {
        if (!imported?.teams.length) return toast.err(t.t("Pick a file with at least one team first"));
        reportBulk(await api.bulkTeams(eventId, imported.teams), importedText, "Imported {{c}}, {{f}} failed: {{err}}");
      } else if (mode === "roster") {
        if (parsed.length === 0) return toast.err(t.t("Add at least one team line"));
        if (offender) return toast.err(t.t("“{{name}}” has no players", { name: offender.name }));
        reportBulk(await api.bulkTeams(eventId, parsed), createdText, "Created {{c}}, {{f}} failed: {{err}}");
      } else if (mode === "single") {
        setTouched({ name: true, names: true });
        const n = name.trim(), list = splitNames(names);
        if (!n || existing.has(n) || list.length === 0) return;
        await api.createTeam(eventId, { name: n, member_names: list, email: email.trim() });
        toast.ok(t.t("Team created"));
      } else {
        const T = Math.max(1, Number.parseInt(count, 10) || 0);
        const P = Math.max(1, Number.parseInt(per, 10) || 0);
        const tp = teamPrefix.trim() || "Team", pp = playerPrefix.trim() || "Player";
        // Step over names already in the event so a second run doesn't fail every create.
        let created = 0, failed = 0, last = "", idx = 1;
        while (created < T && idx < 10_000) {
          let n: string;
          do n = `${tp} ${idx++}`; while (existing.has(n) && idx < 10_000);
          if (idx >= 10_000) break;
          try {
            await api.createTeam(eventId, { name: n, member_names: Array.from({ length: P }, (_, j) => `${pp} ${j + 1}`) });
            created++;
            existing.add(n);
          } catch (e) {
            failed++;
            last = errorText(e);
            // Anything but a name clash (event full, permission) won't get better by retrying.
            if (!/already used/i.test(last)) break;
          }
        }
        if (created === 0 && failed > 0) toast.err(last || t.t("Could not create any teams"));
        else if (failed) toast.err(t.t("Created {{c}}, {{f}} failed: {{err}}", { c: created, f: failed, err: last }));
        else toast.ok(createdText(created));
        if (created === 0) return;
      }
      onDone();
      onClose();
    } catch (e) {
      toast.err(e);
    } finally {
      setBusy(false);
    }
  };

  const modes: { key: Mode; label: string }[] = [
    { key: "single", label: t.t("One team") },
    { key: "bulk", label: t.t("Many teams at once") },
    { key: "roster", label: t.t("Paste roster") },
    { key: "import", label: t.t("Upload file") },
  ];
  const T = Number.parseInt(count, 10) || 0, P = Number.parseInt(per, 10) || 0;

  return (
    <Dialog
      title={t.t("Add teams")}
      wide
      onClose={() => void close()}
      onSubmit={() => void submit()}
      footer={
        <>
          <Button onClick={() => void close()}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Create")}</Button>
        </>
      }
    >
      <fieldset className="ev-fieldset">
        <legend className="cs-sr">{t.t("Add teams")}</legend>
        {modes.map((m) => (
          <label key={m.key} className="ev-check">
            <input type="radio" name="team-mode" checked={mode === m.key} onChange={() => setMode(m.key)} />
            {m.label}
          </label>
        ))}
      </fieldset>

      {mode === "single" && (
        <>
          <Field label={t.t("Team name")} error={nameErr}>
            <input value={name} autoFocus onChange={(e) => setName(e.target.value)} onBlur={() => setTouched((s) => ({ ...s, name: true }))} />
          </Field>
          <Field label={t.t("Members — one per line, or paste CSV")} error={namesErr} hint={t.t("Each line becomes a player slot with its own magic-link token.")}>
            <textarea rows={5} placeholder={"alice\nbob\ncharlie"} value={names} onChange={(e) => setNames(e.target.value)} onBlur={() => setTouched((s) => ({ ...s, names: true }))} />
          </Field>
          <Field label={t.t("Captain email (optional)")} hint={t.t("Used by the arranger only — never shown on the team's share page.")}>
            <input type="email" placeholder="captain@example.org" autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
        </>
      )}

      {mode === "bulk" && (
        <>
          <div className="ev-grid2">
            <Field label={t.t("Number of teams")}><input type="number" min={1} max={50} value={count} onChange={(e) => setCount(e.target.value)} /></Field>
            <Field label={t.t("Players per team")}><input type="number" min={1} max={20} value={per} onChange={(e) => setPer(e.target.value)} /></Field>
            <Field label={t.t("Team name prefix")}><input value={teamPrefix} onChange={(e) => setTeamPrefix(e.target.value)} /></Field>
            <Field label={t.t("Player name prefix")}><input value={playerPrefix} onChange={(e) => setPlayerPrefix(e.target.value)} /></Field>
          </div>
          <p className="ev-muted">{t.t("Creates {{teams}} × {{players}} = {{links}}.", {
            teams: t.plural(T, { one: t.t("{{n}} team"), other: t.t("{{n}} teams") }),
            players: t.plural(P, { one: t.t("{{n}} player"), other: t.t("{{n}} players") }),
            links: t.plural(T * P, { one: t.t("{{n}} magic link"), other: t.t("{{n}} magic links") }),
          })}</p>
        </>
      )}

      {mode === "roster" && (
        <Field
          label={t.t("Roster — one team per line")}
          error={offender ? t.t('"{{name}}" has no player names — add at least one after the team name.', { name: offender.name }) : null}
          hint={parsed.length
            ? sizes(parsed.length, parsed.reduce((n, tm) => n + tm.member_names.length, 0))
            : t.t("First column is the team name; remaining columns become player slots. Comma, tab, semicolon and colon are all accepted as separators.")}
        >
          <textarea rows={8} autoFocus placeholder={"Alpha, alice, bob, carol\nBeta, dave, ezra\nGamma: fiona, gina, henry"} value={roster} onChange={(e) => setRoster(e.target.value)} />
        </Field>
      )}

      {mode === "import" && (
        <Field
          label={t.t("Roster file")}
          error={importMsg?.err ? importMsg.text : null}
          hint={importMsg && !importMsg.err ? importMsg.text : t.t("Supported: CSV, Excel (.xlsx), OpenDocument (.ods), IOF XML. First column is the team name; the rest become player slots.")}
        >
          <input type="file" accept=".csv,.ods,.xlsx,.xml" onChange={(e) => void readFile(e.target.files?.[0])} />
        </Field>
      )}
    </Dialog>
  );
}
