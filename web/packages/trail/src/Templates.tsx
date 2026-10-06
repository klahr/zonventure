import type { I18n } from "@zonventure/i18n";
import { Icon } from "./Icons.tsx";

/** A template a trail starts from: internal/admin/trails.go trailTemplateDTO. */
export interface TemplateInfo {
  name: string;
  title: string;
  kids: boolean;
  cards: number;
}

const ABOUT: Record<string, { label: string; blurb: string; icon: string }> = {
  birthday: { label: "Birthday party", blurb: "Questions about the birthday child, for the guests to answer.", icon: "cake" },
  kids: { label: "Kids' treasure hunt", blurb: "Easy questions for children, with a treasure at the end.", icon: "compass" },
  office: { label: "Office or team", blurb: "Questions about the workplace, for a team day or a party.", icon: "briefcase" },
  family: { label: "Family", blurb: "Questions about the family, for a get-together.", icon: "home" },
  blank: { label: "Blank", blurb: "Start from one empty card.", icon: "plus" },
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
            <button type="button" className={"trail-template trail-template--" + tp.name} disabled={busy} onClick={() => onPick(tp.name)}>
              <span className="trail-template__art" aria-hidden="true">
                <Icon name={about?.icon ?? "sparkle"} size={26} />
              </span>
              <span className="trail-template__body">
                <strong>{about ? t(about.label) : tp.title}</strong>
                <span>{about ? t(about.blurb) : ""}</span>
                {tp.name !== "blank" && <small>{t("{{n}} example cards", { n: tp.cards })}</small>}
              </span>
              <span className="trail-template__go" aria-hidden="true">
                <Icon name="right" size={18} />
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
