import type { I18n } from "@zonventure/i18n";

/** A template a trail starts from: internal/admin/trails.go trailTemplateDTO. */
export interface TemplateInfo {
  name: string;
  title: string;
  kids: boolean;
  cards: number;
}

const ABOUT: Record<string, { label: string; blurb: string }> = {
  birthday: { label: "Birthday party", blurb: "Questions about the birthday child, for the guests to answer." },
  kids: { label: "Kids' trail", blurb: "Easy questions for children, with a treasure at the end." },
  office: { label: "Office or team", blurb: "Questions about the workplace, for a team day or a party." },
  family: { label: "Family", blurb: "Questions about the family, for a get-together." },
  blank: { label: "Blank", blurb: "Start from one empty card." },
};

/** The templates to pick from, each with what it is for. */
export function TemplatePicker({ i18n, templates, busy, onPick }: { i18n: I18n; templates: TemplateInfo[]; busy?: boolean; onPick: (name: string) => void }) {
  const t = i18n.t;
  return (
    <ul className="trail-templates">
      {templates.map((tp) => {
        const about = ABOUT[tp.name];
        return (
          <li key={tp.name}>
            <button type="button" disabled={busy} onClick={() => onPick(tp.name)}>
              <strong>{about ? t(about.label) : tp.title}</strong>
              <span>{about ? t(about.blurb) : ""}</span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
