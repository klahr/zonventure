export * from "./cards.ts";
export * from "./save.ts";
export { Editor, type EditorProps } from "./Editor.tsx";
export { TemplatePicker, type TemplateInfo } from "./Templates.tsx";
import sv from "./sv.json" with { type: "json" };
/** The package's Swedish strings, to merge into the app's sv catalog. */
export const trailSv: Record<string, string> = sv;
export { TRAIL_THEMES, TEMPLATE_THEMES, themeById, themeStyle, type TrailTheme } from "./themes.ts";
export { ThemePicker, ThemeShowcase, PaperPreview } from "./Themes.tsx";
