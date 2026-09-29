import { useEffect, useId, useRef, useState } from "react";
import { Trash2 } from "lucide-react";
import { useT } from "../../i18n.tsx";
import { useSession } from "../../session.tsx";
import { Button, Callout, Dialog, Field, ICON, fmtMoney, useToast } from "../../ui/index.ts";
import {
  addMember, createAssignment, deleteAssignment, grantSlots, renameOrganization, searchUsers, updateAssignment,
  type Assignment, type CatalogAdventure, type OrgEvent, type Organization,
} from "./api.ts";

type Done = { onClose: () => void; onDone: () => void };

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
  const [username, setUsername] = useState("");
  const [picked, setPicked] = useState<Match | null>(null);
  const [matches, setMatches] = useState<Match[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("org_member");
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
      await addMember(org.id, body);
      toast.ok(t.t("User added"));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };

  const status = picked
    ? t.t("Adding existing user — they keep their current password.")
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
      <div className="ad-combo">
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
          <ul className="ad-combo__list" id={listId} role="listbox">
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
      <p className={"ad-status" + (picked ? " is-ok" : "")} aria-live="polite">{status}</p>
      {!picked && (
        <>
          <Field label={t.t("Email (optional)")} hint={t.t("Lets the user log in with either their username or email, and gives them a contact address for password reset.")}>
            <input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label={t.t("Password")} hint={t.t("Minimum 8 characters.")}>
            <input type="password" autoComplete="new-password" minLength={8} required={!!username.trim()} value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        </>
      )}
      <Field label={t.t("Role")}>
        <select value={role} onChange={(e) => setRole(e.target.value)}>
          <option value="org_member">{t.t("Event host")}</option>
          <option value="org_admin">{t.t("Admin")}</option>
        </select>
      </Field>
    </Dialog>
  );
}

const advLabel = (a: CatalogAdventure) => `${a.title} (${a.slug})`;

export function GrantPassDialog({ org, adventures, onClose, onDone }: Done & { org: Organization; adventures: CatalogAdventure[] }) {
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
      title={t.t("Assign adventure to {{name}}", { name: org.name })}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Assign")}</Button></>}
    >
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

export function EditPassDialog({ org, pass, adventures, onClose, onDone }: Done & { org: Organization; pass: Assignment; adventures: CatalogAdventure[] }) {
  const t = useT();
  const toast = useToast();
  const used = !!pass.event_id;
  const [slug, setSlug] = useState(pass.adventure_slug);
  const [max, setMax] = useState(String(pass.max_players));
  const [busy, setBusy] = useState(false);
  const options = adventures.some((a) => a.slug === pass.adventure_slug)
    ? adventures
    : [{ slug: pass.adventure_slug, title: pass.adventure_title || pass.adventure_slug } as CatalogAdventure, ...adventures];
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
          {!used && <Button variant="danger" icon={<Trash2 size={ICON.md} aria-hidden />} onClick={revoke} disabled={busy} className="ad-foot-start">{t.t("Revoke")}</Button>}
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

/**
 * Add players to a live event's pass on the org's behalf. Only an org on
 * invoice billing can be charged; for the rest the grant is free, as the
 * server would force it anyway.
 */
export function GrantSlotsDialog({ org, event, onClose, onDone }: Done & { org: Organization; event: OrgEvent }) {
  const t = useT();
  const toast = useToast();
  const { state } = useSession();
  const pricing = (state.status === "authenticated" ? state.user.pricing : undefined) as { player_cents?: number; currency?: string } | undefined;
  const PRICE = pricing?.player_cents || 500;
  const CURRENCY = pricing?.currency || "EUR";
  const trusted = org.trusted;
  const [players, setPlayers] = useState("5");
  const suggest = (p: string) => String(Math.max(1, Number.parseInt(p, 10) || 0) * PRICE);
  const [price, setPrice] = useState(trusted ? suggest("5") : "0");
  const [busy, setBusy] = useState(false);
  const title = event.adventure_title || event.adventure_slug || t.t("this event");
  const submit = async () => {
    const added = Math.max(1, Number.parseInt(players, 10) || 0);
    const amount = trusted ? Math.max(0, Number.parseInt(price, 10) || 0) : 0;
    setBusy(true);
    try {
      await grantSlots(event.id, added, amount);
      toast.ok(amount > 0
        ? t.t("{{n}} players added — {{price}} on next invoice", { n: added, price: fmtMoney(amount, CURRENCY, t.current()) })
        : t.t("{{n}} players added to the pass (free)", { n: added }));
      onDone();
    } catch (e) {
      toast.err(e);
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t.t("Add players")}
      onClose={onClose}
      onSubmit={submit}
      footer={<><Button onClick={onClose}>{t.t("Cancel")}</Button><Button type="submit" variant="primary" loading={busy}>{t.t("Add players")}</Button></>}
    >
      <p>{t.t("The event pass for \"{{title}}\" covers {{n}} players. How many more?", { title, n: event.max_players || 0 })}</p>
      <Callout variant="info">
        {trusted
          ? t.t("Adding to \"{{org}}\" — the price below appears on their next invoice. Set it to 0 for a free grant.", { org: org.name })
          : t.t("\"{{org}}\" is not on invoice billing, so this grant is free. Flip the org's \"Invoice billing\" switch in Organizations to enable priced grants.", { org: org.name })}
      </Callout>
      <Field label={t.t("Additional players")}>
        <input type="number" min={1} max={500} required autoFocus value={players} onChange={(e) => { setPlayers(e.target.value); if (trusted) setPrice(suggest(e.target.value)); }} />
      </Field>
      {trusted && (
        <Field
          label={t.t("Price (cents)")}
          hint={`${fmtMoney(Math.max(0, Number.parseInt(price, 10) || 0), CURRENCY, t.current())} · ${t.t("Whatever you enter here lands on the next invoice. Zero is fine — that's a free goodwill grant.")}`}
        >
          <input type="number" min={0} step={1} value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
      )}
    </Dialog>
  );
}

/** Pick one of a list: an adventure to commission, or an organisation to share with. */
export function PickDialog<T>({ title, items, label, onPick, onClose }: { title: string; items: T[]; label: (x: T) => string; onPick: (x: T) => void; onClose: () => void }) {
  const t = useT();
  return (
    <Dialog title={title} onClose={onClose} footer={<Button onClick={onClose}>{t.t("Cancel")}</Button>}>
      <p className="ad-muted">{t.t("Pick one. The organization can schedule events of this adventure without buying event passes.")}</p>
      <div className="ad-picklist">
        {items.map((x, i) => (
          <Button key={i} block autoFocus={i === 0} onClick={() => onPick(x)}>{label(x)}</Button>
        ))}
      </div>
    </Dialog>
  );
}
