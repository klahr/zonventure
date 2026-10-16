import { useEffect, useId, useRef, useState } from "react";
import { Trash2 } from "lucide-react";
import { ApiError } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Dialog, Field, ICON, useToast } from "../../ui/index.ts";
import {
  addMember, createAssignment, deleteAssignment, renameOrganization, searchUsers, setBilled, setUserPassword, updateAssignment,
  type Assignment, type CatalogAdventure, type OrgMember, type Organization,
} from "./api.ts";

type Done = { onClose: () => void; onDone: () => void };

/** A new password for a user, e.g. one without an email who can't reset it. */
export function SetPasswordDialog({ member, onClose, onDone }: Done & { member: Pick<OrgMember, "id" | "username"> }) {
  const t = useT();
  const toast = useToast();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!password) return;
    setBusy(true);
    try {
      await setUserPassword(member.id, password);
      toast.ok(t.t("Password set. Give it to {{username}} in person or by phone.", { username: member.username }));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Set a password for {{username}}", { username: member.username })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy} disabled={!password}>{t.t("Set password")}</Button></>}
    >
      <Field label={t.t("New password")}>
        <input type="text" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} required autoFocus />
      </Field>
    </Dialog>
  );
}

export function RenameOrgDialog({ org, onClose, onDone }: Done & { org: Organization }) {
  const t = useT();
  const toast = useToast();
  const [name, setName] = useState(org.name);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!name.trim()) return;
    setBusy(true);
    try {
      await renameOrganization(org.id, name.trim());
      toast.ok(t.t("Renamed"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Rename {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy} disabled={!name.trim()}>{t.t("Save")}</Button></>}
    >
      <Field label={t.t("New name")}>
        <input value={name} onChange={(e) => setName(e.target.value)} required autoFocus maxLength={200} />
      </Field>
    </Dialog>
  );
}

/** Mark an invoice purchase billed, with the off-platform invoice's number the org then sees. */
export function MarkBilledDialog({ purchaseId, onClose, onDone }: Done & { purchaseId: number }) {
  const t = useT();
  const toast = useToast();
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true);
    try {
      await setBilled(purchaseId, true, number.trim());
      toast.ok(t.t("Marked billed"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Mark billed")}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Mark billed")}</Button></>}
    >
      <Field label={t.t("Invoice number (optional)")} hint={t.t("The org sees it beside the purchase.")}>
        <input value={number} onChange={(e) => setNumber(e.target.value)} autoFocus maxLength={100} />
      </Field>
    </Dialog>
  );
}

type Match = { id: number; username: string };

/**
 * Add a user: pick an existing account from the typeahead (it keeps its
 * password), or type a new username and set one. A typed name that
 * exactly matches a suggestion counts as picked, so "I know the
 * username" never ends in a confusing "set a password".
 */
export function AddMemberDialog({ org, onClose, onDone }: Done & { org: Organization }) {
  const t = useT();
  const toast = useToast();
  const listId = useId();
  const emailHint = useId();
  const [username, setUsername] = useState("");
  const [picked, setPicked] = useState<Match | null>(null);
  const [matches, setMatches] = useState<Match[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [passwordError, setPasswordError] = useState("");
  // The first admin is the likely first user; after that, least privilege.
  const [role, setRole] = useState((org.members ?? []).some((m) => m.role === "org_admin") ? "org_member" : "org_admin");
  const [busy, setBusy] = useState(false);
  const last = useRef<{ q: string; users: Match[] }>({ q: "", users: [] });
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const current = useRef("");
  current.current = username;

  useEffect(() => () => clearTimeout(timer.current), []);

  const search = async (q: string): Promise<Match[]> => {
    if (q.length < 2) return [];
    if (last.current.q === q) return last.current.users;
    try {
      const users = await searchUsers(q, org.id);
      last.current = { q, users };
      // Typed on since: a stale answer must not show.
      if (q === current.current.trim()) {
        setMatches(users);
        setActive(-1);
        setOpen(users.length > 0);
      }
      return users;
    } catch {
      return [];
    }
  };

  const autoPick = async (): Promise<Match | null> => {
    clearTimeout(timer.current);
    const q = username.trim();
    if (!q) return null;
    const users = await search(q);
    const m = users.find((u) => u.username.toLowerCase() === q.toLowerCase()) ?? null;
    if (m) {
      setPicked(m);
      setUsername(m.username);
    }
    return m;
  };

  const choose = (m: Match) => {
    setPicked(m);
    setUsername(m.username);
    setOpen(false);
  };

  const submit = async () => {
    const u = username.trim();
    if (!u) return;
    const who = picked ?? (await autoPick());
    const body: { username: string; role: string; password?: string; email?: string } = { username: who?.username ?? u, role };
    if (!who) {
      if (!password) {
        toast.err(t.t("Set a password for the new account, or pick an existing user from the suggestions."));
        return;
      }
      body.password = password;
      if (email.trim()) body.email = email.trim();
    }
    setBusy(true);
    try {
      // The server answers alike for a new account and an invitation.
      await addMember(org.id, body);
      toast.ok(who ? t.t("Invitation sent to {{name}}. They join once they accept.", { name: body.username }) : body.email ? t.t("If this address has an account, they get an invitation; otherwise a new account is created.") : t.t("User added"));
      onDone();
    } catch (e) {
      // Said at the field, not in a toast behind the dialog.
      if (e instanceof ApiError && e.code === "password_too_common") setPasswordError(t.t("That password is too common. Pick something harder to guess."));
      else toast.err(e);
      setBusy(false);
    }
  };

  const status = picked
    ? t.t("Inviting an existing user — they join once they accept, and keep their password.")
    : username.trim()
      ? t.t("Will create a new account — set an initial password below.")
      : "";

  return (
    <Dialog
      title={t.t("Add user to {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy} disabled={!username.trim()}>{t.t("Add")}</Button></>}
    >
      <div className="adm-combo">
        <Field label={t.t("Username")}>
          <input
            value={username}
            required
            autoFocus
            autoComplete="off"
            spellCheck={false}
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={listId}
            {...(open && active >= 0 ? { "aria-activedescendant": `${listId}-${active}` } : {})}
            onChange={(e) => {
              const v = e.target.value;
              setUsername(v);
              setPicked(null);
              clearTimeout(timer.current);
              if (v.trim().length < 2) setOpen(false);
              else timer.current = setTimeout(() => void search(v.trim()), 200);
            }}
            onFocus={() => matches.length > 0 && username.trim().length >= 2 && !picked && setOpen(true)}
            onBlur={() => {
              setTimeout(() => setOpen(false), 100);
              if (!picked) void autoPick();
            }}
            onKeyDown={(e) => {
              if (!open || matches.length === 0) return;
              if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => (a + 1) % matches.length); }
              else if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => (a <= 0 ? matches.length - 1 : a - 1)); }
              else if (e.key === "Enter" && active >= 0) { e.preventDefault(); choose(matches[active]!); }
            }}
          />
        </Field>
        {open && (
          <ul className="adm-combo__list" id={listId} role="listbox">
            {matches.map((m, i) => (
              <li
                key={m.id}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                className={i === active ? "is-active" : undefined}
                onMouseDown={(e) => { e.preventDefault(); choose(m); }}
              >
                {m.username}
              </li>
            ))}
          </ul>
        )}
      </div>
      <p className={"adm-status" + (picked ? " is-ok" : "")} aria-live="polite">{status}</p>
      {!picked && (
        <>
          {/* The hint names the username and password: kept out of the field's name so it is not taken for theirs. */}
          <Field label={t.t("Email (optional)")} hint={<span id={emailHint}>{t.t("Lets the user log in with either their username or email, and gives them a contact address for password reset.")}</span>}>
            <input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} aria-label={t.t("Email (optional)")} aria-describedby={emailHint} />
          </Field>
          <Field label={t.t("Password")} hint={t.t("Minimum 8 characters.")} {...(passwordError ? { error: passwordError } : {})}>
            <input type="password" autoComplete="new-password" minLength={8} required={!!username.trim()} value={password} onChange={(e) => { setPassword(e.target.value); setPasswordError(""); }} />
          </Field>
        </>
      )}
      <Field label={t.t("Role")}>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="org_member">{t.t("Event host")}</option>
          <option value="org_writer">{t.t("Writer")}</option>
          <option value="org_admin">{t.t("Admin")}</option>
        </select>
      </Field>
    </Dialog>
  );
}

/** An adventure an event pass can be granted for: the catalog's and the org's own published ones. */
type PassAdventure = Pick<CatalogAdventure, "slug" | "title">;

const advLabel = (a: PassAdventure) => `${a.title} (${a.slug})`;

export function GrantPassDialog({ org, adventures, onClose, onDone }: Done & { org: Organization; adventures: PassAdventure[] }) {
  const t = useT();
  const toast = useToast();
  const [slug, setSlug] = useState(adventures[0]?.slug ?? "");
  const [max, setMax] = useState("20");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    const n = Number.parseInt(max, 10);
    if (!(n > 0)) { toast.err(t.t("Max players must be at least 1")); return; }
    setBusy(true);
    try {
      await createAssignment(org.id, slug, n);
      toast.ok(t.t("Event pass granted"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Grant an event pass to {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Grant event pass")}</Button></>}
    >
      <p>{t.t("A free event pass: they can run one event of this adventure. Nothing is charged.")}</p>
      <Field label={t.t("Adventure")}>
        <select value={slug} onChange={(e) => setSlug(e.target.value)} autoFocus>
          {adventures.map((a) => <option key={a.slug} value={a.slug}>{advLabel(a)}</option>)}
        </select>
      </Field>
      <Field label={t.t("Maximum players")} hint={t.t("Each event pass is used for exactly one event. For more, grant another.")}>
        <input type="number" min={1} step={1} required value={max} onChange={(e) => setMax(e.target.value)} />
      </Field>
    </Dialog>
  );
}

export function EditPassDialog({ org, pass, adventures, onClose, onDone }: Done & { org: Organization; pass: Assignment; adventures: PassAdventure[] }) {
  const t = useT();
  const toast = useToast();
  const used = !!pass.event_id;
  const [slug, setSlug] = useState(pass.adventure_slug);
  const [max, setMax] = useState(String(pass.max_players));
  const [busy, setBusy] = useState(false);
  const options = adventures.some((a) => a.slug === pass.adventure_slug)
    ? adventures
    : [{ slug: pass.adventure_slug, title: pass.adventure_title || pass.adventure_slug }, ...adventures];
  const submit = async () => {
    const n = Number.parseInt(max, 10);
    if (!(n > 0)) { toast.err(t.t("Max players must be at least 1")); return; }
    const body: { max_players?: number; adventure_slug?: string } = {};
    if (n !== pass.max_players) body.max_players = n;
    if (!used && slug !== pass.adventure_slug) body.adventure_slug = slug;
    if (Object.keys(body).length === 0) { onClose(); return; }
    setBusy(true);
    try {
      await updateAssignment(org.id, pass.id, body);
      toast.ok(t.t("Event pass updated"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  const revoke = async () => {
    setBusy(true);
    try {
      await deleteAssignment(org.id, pass.id);
      toast.ok(t.t("Event pass revoked"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Edit event pass · {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={
        <>
          {!used && <Button variant="danger" icon={<Trash2 size={ICON.md} aria-hidden />} onClick={revoke} disabled={busy} className="adm-foot-start">{t.t("Revoke")}</Button>}
          <Button onClick={onClose}>{t.t("Cancel")}</Button>
          <Button type="submit" variant="primary" loading={busy}>{t.t("Save")}</Button>
        </>
      }
    >
      <Field label={used ? t.t("Adventure (locked — already used)") : t.t("Adventure")}>
        <select value={slug} onChange={(e) => setSlug(e.target.value)} disabled={used}>
          {options.map((a) => <option key={a.slug} value={a.slug}>{advLabel(a)}</option>)}
        </select>
      </Field>
      <Field
        label={t.t("Maximum players")}
        hint={used
          ? t.t("Linked to event #{{id}}. You may raise the cap; lowering below the current player-slot count is rejected.", { id: pass.event_id! })
          : t.t("This event pass has not been used yet.")}
      >
        <input type="number" min={1} step={1} required value={max} onChange={(e) => setMax(e.target.value)} autoFocus />
      </Field>
    </Dialog>
  );
}

/** Pick one of a list: an adventure to commission, or an organisation to share with. */
export function PickDialog<T>({ title, items, label, onPick, onClose }: { title: string; items: T[]; label: (x: T) => string; onPick: (x: T) => void; onClose: () => void }) {
  const t = useT();
  return (
    <Dialog title={title} onClose={onClose} footer={<Button onClick={onClose}>{t.t("Cancel")}</Button>}>
      <p className="cs-note cs-note--md">{t.t("Pick one. It shows among the organization's own adventures.")}</p>
      <div className="adm-picklist">
        {items.map((x, i) => (
          <Button key={i} block autoFocus={i === 0} onClick={() => onPick(x)}>{label(x)}</Button>
        ))}
      </div>
    </Dialog>
  );
}
