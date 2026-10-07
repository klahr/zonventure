import { useEffect, useRef, useState } from "react";
import { Copy, Trash2 } from "lucide-react";
import { adventures, type play } from "@zonventure/api";
import { useT } from "../../i18n.tsx";
import { Button, Card, ErrorBox, ICON, Loading, errorText, useDialogs, useToast } from "../../ui/index.ts";
import { SamplePreview } from "../../designer/SamplePreview.tsx";
import { StarterGallery } from "../../designer/StarterGallery.tsx";
import { ThemeEditor, ThemeStrip } from "../../designer/ThemeEditor.tsx";
import "../../designer/dz.css";
import "../../designer/designer.css";

/** A name the library does not have yet: the name itself, else with 2, 3, … after it. */
function freeName(themes: adventures.OrgTheme[], name: string): string {
  const taken = new Set(themes.map((th) => th.name.toLowerCase()));
  if (!taken.has(name.toLowerCase())) return name;
  for (let i = 2; ; i++) if (!taken.has(`${name} ${i}`.toLowerCase())) return `${name} ${i}`;
}

/**
 * The organisation's theme library, outside any adventure: the looks its
 * adventures start new themes from. An adventure takes a copy, so a change
 * here reaches the next copy, never an adventure already using one. Every
 * change saves by itself a moment after the last edit. Pictures and sounds
 * are an adventure's own, so a library theme has none.
 */
export function ThemesTab() {
  const t = useT();
  const toast = useToast();
  const dialogs = useDialogs();
  const [themes, setThemes] = useState<adventures.OrgTheme[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [current, setCurrent] = useState<number | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [status, setStatus] = useState<{ id: number; text: string } | null>(null);
  const latest = useRef(themes);
  latest.current = themes;
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>());

  useEffect(() => {
    const ac = new AbortController();
    adventures.listOrgThemes(ac.signal).then(
      (list) => {
        setThemes(list);
        setCurrent(list[0]?.id ?? null);
      },
      (e: unknown) => !ac.signal.aborted && setLoadError(errorText(e)),
    );
    const pending = timers.current;
    return () => {
      ac.abort();
      for (const id of pending.values()) clearTimeout(id);
    };
  }, []);

  if (loadError) return <ErrorBox message={loadError} />;
  if (!themes) return <Loading label={t.t("Loading…")} />;
  const theme = themes.find((th) => th.id === current) ?? null;

  const persist = async (id: number) => {
    timers.current.delete(id);
    const th = latest.current?.find((x) => x.id === id);
    if (!th || !th.name.trim()) return;
    try {
      await adventures.updateOrgTheme(id, th.name.trim(), th.look);
      setStatus({ id, text: t.t("Saved") });
    } catch (e) {
      setStatus(null);
      toast.err(e);
    }
  };
  const edit = (id: number, patch: Partial<adventures.OrgTheme>) => {
    setThemes((list) => list && list.map((th) => (th.id === id ? { ...th, ...patch } : th)));
    setStatus({ id, text: t.t("Saving…") });
    clearTimeout(timers.current.get(id));
    timers.current.set(id, setTimeout(() => void persist(id), 600));
  };
  const create = async (name: string, look: play.Brand) => {
    try {
      const r = await adventures.createOrgTheme(freeName(themes, name), structuredClone(look));
      setThemes((list) => [...(list ?? []), r.theme].sort((a, b) => a.name.localeCompare(b.name)));
      setCurrent(r.theme.id);
      setChoosing(false);
      setStatus(null);
    } catch (e) {
      toast.err(e);
    }
  };
  const remove = async (th: adventures.OrgTheme) => {
    const go = await dialogs.confirm({
      title: t.t("Remove “{{name}}” from the organisation's themes?", { name: th.name }),
      message: t.t("Adventures that use it keep their copy."),
      confirmLabel: t.t("Remove"),
      danger: true,
    });
    if (!go) return;
    try {
      clearTimeout(timers.current.get(th.id));
      await adventures.deleteOrgTheme(th.id);
      const next = themes.filter((x) => x.id !== th.id);
      setThemes(next);
      setCurrent(next[0]?.id ?? null);
    } catch (e) {
      toast.err(e);
    }
  };

  return (
    <Card
      title={t.t("Theme library")}
      subtitle={t.t("Your organisation's adventures can start a theme from these. Each adventure keeps its own copy, so a change here reaches new copies, not adventures already using the theme.")}
    >
      <div className="themes st-themes">
        <ThemeStrip
          themes={themes.map((th) => ({ key: String(th.id), name: th.name, look: th.look }))}
          current={current === null ? null : String(current)}
          adding={choosing || !theme}
          onPick={(k) => {
            setCurrent(Number(k));
            setChoosing(false);
          }}
          onAdd={() => setChoosing(true)}
        />
        {choosing || !theme ? (
          <StarterGallery
            onPick={(name, look) => void create(name || t.t("Theme {{n}}", { n: themes.length + 1 }), look)}
            ownLookHint={false}
            {...(theme ? { onCancel: () => setChoosing(false) } : {})}
          />
        ) : (
          <ThemeEditor
            name={theme.name}
            onName={(name) => edit(theme.id, { name })}
            look={theme.look}
            onLook={(look) => edit(theme.id, { look })}
            media={false}
            actions={
              <>
                <Button variant="ghost" size="sm" icon={<Copy size={ICON.sm} aria-hidden />} onClick={() => void create(t.t("{{name}} (copy)", { name: theme.name }), theme.look)}>
                  {t.t("Duplicate")}
                </Button>
                <Button variant="ghost" size="sm" icon={<Trash2 size={ICON.sm} aria-hidden />} onClick={() => void remove(theme)}>
                  {t.t("Delete")}
                </Button>
              </>
            }
            note={
              <p className="cs-field__hint theme-ed__note" role="status">
                {!theme.name.trim() ? t.t("A theme needs a name.") : status?.id === theme.id ? status.text : t.t("Changes save by themselves.")}
              </p>
            }
            preview={<SamplePreview brand={theme.look} />}
          />
        )}
      </div>
    </Card>
  );
}
