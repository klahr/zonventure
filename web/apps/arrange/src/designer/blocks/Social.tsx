import { Plus, X } from "lucide-react";
import { Button, ICON } from "../dz/ui.tsx";
import { withField, type Editor, type EditorProps, type T } from "./types.ts";
import { MarkdownArea } from "../MarkdownArea.tsx";
import { focusAdded } from "../fields.tsx";

/** A list of texts that keeps at least `min`: a split secret's parts, a vote's options, a draw's cards. */
function TextList({ items, set, min, rows, placeholder, label, addLabel, t }: { items: string[]; set: (next: string[]) => void; min: number; rows: number; placeholder: string; label: (i: number) => string; addLabel: string; t: T }) {
  const change = (i: number, v: string) => set(items.map((x, j) => (j === i ? v : x)));
  return (
    <div className="list-ed">
      {items.map((s, i) => (
        <div key={i} className="list-ed__item">
          <div className="list-ed__body">
            {rows > 1 ? (
              <MarkdownArea className="field__input" rows={rows} value={s} placeholder={placeholder} aria-label={label(i)} onChange={(e) => change(i, e.target.value)} />
            ) : (
              <input className="field__input" value={s} placeholder={placeholder} aria-label={label(i)} onChange={(e) => change(i, e.target.value)} />
            )}
          </div>
          <Button variant="ghost" size="icon" className="danger" disabled={items.length <= min} onClick={() => set(items.filter((_, j) => j !== i))} aria-label={t.t("Remove")}>
            <X size={ICON.sm} />
          </Button>
        </div>
      ))}
      <button type="button" className="add-btn" onClick={(e) => {
        // A blank row is filled before another is added: blanks would show as empty buttons.
        const blank = items.findIndex((s) => !s.trim());
        if (blank >= 0) {
          e.currentTarget.parentElement?.querySelectorAll<HTMLElement>(":scope > .list-ed__item")[blank]?.querySelector<HTMLElement>("input, textarea")?.focus();
          return;
        }
        set([...items, ""]);
        focusAdded(e.currentTarget.parentElement, ":scope > .list-ed__item");
      }}>
        <Plus size={ICON.sm} aria-hidden="true" />
        {addLabel}
      </button>
    </div>
  );
}

export const SplitEditor: Editor<"split"> = ({ block, set, t }) => (
  <>
    <TextList items={block.parts ?? []} set={(parts) => set({ ...block, parts })} min={2} rows={2} placeholder={t.t("Markdown. One piece of the secret.")} label={(i) => t.t("Part {{n}}", { n: i + 1 })} addLabel={t.t("Add part")} t={t} />
    <p className="field__hint">{t.t("Each player's phone shows one part; with fewer players than parts some get several, and a player alone sees all.")}</p>
  </>
);

export const VoteEditor: Editor<"vote"> = ({ block, set, t }) => (
  <>
    <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("The question, such as “Which door do we open?”")} aria-label={t.t("Question")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <TextList items={block.options ?? []} set={(options) => set({ ...block, options })} min={2} rows={1} placeholder={t.t("An option")} label={(i) => t.t("Option {{n}}", { n: i + 1 })} addLabel={t.t("Add option")} t={t} />
    <p className="field__hint">{t.t("Each player votes from their own phone; everyone sees the tally. A vote never routes.")}</p>
  </>
);

export const DrawEditor: Editor<"draw"> = ({ block, set, t }) => (
  <>
    <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("Markdown. Such as “Draw your fortune.”")} aria-label={t.t("Prompt")} onChange={(e) => set(withField(block, "text", e.target.value))} />
    <TextList items={block.options ?? []} set={(options) => set({ ...block, options })} min={2} rows={1} placeholder={t.t("A card")} label={(i) => t.t("Card {{n}}", { n: i + 1 })} addLabel={t.t("Add card")} t={t} />
    <p className="field__hint">{t.t("The team draws one card at random, once. A show condition can ask which card was drawn.")}</p>
  </>
);

export const PhotoEditor: Editor<"photo"> = ({ block, set, t }) => (
  <>
    <MarkdownArea
      className="field__input"
      rows={2}
      value={block.text ?? ""}
      placeholder={t.t("Such as “Take a picture of your team with the statue in the background.”")}
      aria-label={t.t("What to photograph")}
      onChange={(e) => set({ ...block, text: e.target.value })}
    />
    <label className="field field--num">
      <span className="field__label">{t.t("Points when approved")}</span>
      <input className="field__input" type="number" min={0} value={block.worth ?? 0} onChange={(e) => set(withField(block, "worth", Math.max(0, Math.round(Number(e.target.value)) || 0) || undefined))} />
    </label>
    <p className="field__hint">
      {t.t("The arranger approves or denies each picture from the event's control page. An approved one adds the points; after a denied one the team can take a new picture. The team never waits for the review.")}
    </p>
  </>
);

export const BulletinEditor: Editor<"bulletin"> = ({ t }) => <p className="field__hint">{t.t("Shows the newest message the organisers sent this team from the event's control page.")}</p>;

/** The secret roles a sabotage or accusation can belong to. */
function SecretRoleSelect({ value, set, graph, t }: { value: string; set: (role: string) => void; graph: EditorProps<"accuse">["graph"]; t: T }) {
  const secret = (graph.roles ?? []).filter((r) => r.secret);
  return (
    <select className="field__input" value={value} onChange={(e) => set(e.target.value)} aria-label={t.t("Secret role")}>
      <option value="">{secret.length ? t.t("— pick the secret role —") : t.t("— no secret role yet: add one under Resources —")}</option>
      {secret.map((r) => (
        <option key={r.slug} value={r.slug}>
          {r.name || r.slug}
        </option>
      ))}
    </select>
  );
}

function NumberInput({ label, value, set }: { label: string; value: number | undefined; set: (n: number | undefined) => void }) {
  return (
    <label className="field field--num">
      <span className="field__label">{label}</span>
      <input className="field__input" type="number" min={0} value={value ?? ""} onChange={(e) => set(e.target.value === "" ? undefined : Math.max(0, Number(e.target.value)))} />
    </label>
  );
}

export const SabotageEditor: Editor<"sabotage"> = ({ block, set, t, graph }) => (
  <>
    <SecretRoleSelect value={block.role} set={(role) => set({ ...block, role })} graph={graph} t={t} />
    <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("What every phone shows, such as “Cut the alarm wire?”")} aria-label={t.t("Text")} onChange={(e) => set(withField(block, "text", e.target.value))} />
    <div className="box__row box__row--wrap">
      <NumberInput label={t.t("Costs the team (points)")} value={block.worth} set={(n) => set(withField(block, "worth", n || undefined))} />
      <NumberInput label={t.t("Locks the answer (s)")} value={block.seconds} set={(n) => set(withField(block, "seconds", n || undefined))} />
    </div>
    <label className="field">
      <span className="field__label">{t.t("Plants a false note")}</span>
      <select className="field__input" value={block.journal ?? ""} onChange={(e) => set(withField(block, "journal", e.target.value || undefined))}>
        <option value="">{t.t("— none —")}</option>
        {(graph.journal ?? []).map((j) => (
          <option key={j.slug} value={j.slug}>
            {j.title || j.slug}
          </option>
        ))}
      </select>
    </label>
    <p className="field__hint">{t.t("Every phone shows it, the same, on a team that was dealt the secret role, and any player may press it once. Only the secret role's press does anything; the others may notice the points go, the lock, or the note, but no screen tells who.")}</p>
  </>
);

export const AccuseEditor: Editor<"accuse"> = ({ block, set, t, graph }) => (
  <>
    <SecretRoleSelect value={block.role} set={(role) => set({ ...block, role })} graph={graph} t={t} />
    <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("The question, such as “Who is the saboteur?”")} aria-label={t.t("Question")} onChange={(e) => set({ ...block, text: e.target.value })} />
    <NumberInput label={t.t("Points if the team names them")} value={block.worth} set={(n) => set(withField(block, "worth", n || undefined))} />
    <p className="field__hint">{t.t("Each player names a teammate on their own phone. If most name the secret role's player, the team scores when it finishes, and the end screen tells all. Shown only to a team that was dealt the role.")}</p>
  </>
);

export const MissionEditor: Editor<"mission"> = ({ block, set, t, graph }) => {
  const options = block.options ?? [];
  const setOption = (i: number, v: string) => {
    const next = [options[0] ?? "", options[1] ?? ""];
    next[i] = v;
    set(withField(block, "options", next.some((o) => o.trim()) ? next : undefined));
  };
  return (
    <>
      <SecretRoleSelect value={block.role} set={(role) => set({ ...block, role })} graph={graph} t={t} />
      <label className="field">
        <span className="field__label">{t.t("Post")}</span>
        <select className="field__input" value={block.zone} onChange={(e) => set({ ...block, zone: e.target.value })}>
          <option value="">{t.t("— pick the zone where players pick —")}</option>
          {(graph.zones ?? []).map((z) => (
            <option key={z.slug} value={z.slug}>
              {z.name || z.slug}
            </option>
          ))}
        </select>
      </label>
      <label className="field field--num">
        <span className="field__label">{t.t("Players who pick")}</span>
        <input className="field__input" type="number" min={1} value={block.count ?? 1} onChange={(e) => set(withField(block, "count", Math.max(1, Math.round(Number(e.target.value)) || 1)))} />
      </label>
      <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("What every phone shows, such as “The package is under the bench. Seal it.”")} aria-label={t.t("Text")} onChange={(e) => set(withField(block, "text", e.target.value))} />
      <div className="box__row box__row--wrap">
        <input className="field__input" value={options[0] ?? ""} placeholder={t.t("Deliver")} aria-label={t.t("The faithful choice")} onChange={(e) => setOption(0, e.target.value)} />
        <input className="field__input" value={options[1] ?? ""} placeholder={t.t("Tamper")} aria-label={t.t("The betrayal")} onChange={(e) => setOption(1, e.target.value)} />
      </div>
      <p className="field__hint">
        {t.t("Each player inside the post picks one, once, until this many have; the step waits until then. Only the secret role's betrayal counts, and every phone looks the same whatever was picked. Ask about it later with the condition “Mission betrayed”.")}
      </p>
    </>
  );
};

export const RolesEditor: Editor<"roles"> = ({ block, set, t, graph }) => (
  <>
    <MarkdownArea className="field__input" rows={2} value={block.text ?? ""} placeholder={t.t("Optional words above the roles, such as “Take your parts before you set off.”")} aria-label={t.t("Text")} onChange={(e) => set(withField(block, "text", e.target.value || undefined))} />
    <label className="check">
      <input type="checkbox" checked={!!block.team} onChange={(e) => set(withField(block, "team", e.target.checked || undefined))} />
      <span>{t.t("Show every player's roles, not only the player's own")}</span>
    </label>
    <label className="check">
      <input type="checkbox" checked={!!block.choose} onChange={(e) => set(withField(block, "choose", e.target.checked || undefined))} />
      <span>{t.t("Let players choose their roles here")}</span>
    </label>
    <label className="check">
      <input type="checkbox" checked={!!block.lock} onChange={(e) => set(withField(block, "lock", e.target.checked || undefined))} />
      <span>{t.t("Roles are final once the team moves on")}</span>
    </label>
    {!(graph.roles ?? []).some((r) => !r.secret) && <div className="alert alert--danger">{t.t("The adventure has no roles yet. Add them in Adventure settings → Roles.")}</div>}
    <p className="field__hint">{t.t("A secret role shows only on its own player's phone, never among the team's. Once roles are final, roles nobody chose stay with whoever holds them, and a player who joins later gets none.")}</p>
  </>
);
