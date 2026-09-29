# Porting the classic console to this app

The arranger console is moving from `internal/server/web/arrange.html`
(vanilla JS, ~19k lines) into this React app, screen by screen, so the
dashboard and the designer are one app with one look. When every screen
is here, `/arrange` redirects to `/app/arrange/` and arrange.html goes.

## Where things go

- **Routes** — `src/router.ts`. Hashes are the classic console's plus a
  leading slash (`#events/5` there is `#/events/5` here); both parse.
- **Pages** — `src/pages/<area>/`. Each area's `index.tsx` exports the
  page components the shell renders (`src/Shell.tsx`, `Page()`); split
  the rest into files in the same folder. Settings has one file per tab
  under `src/pages/settings/`.
- **API calls** — `src/pages/<area>/api.ts`, built on `get`, `post`,
  `put`, `patch`, `request` from `@zonventure/api`. Type the responses
  from the Go handler structs (cite the file). Don't add area calls to
  `web/packages/api`; that package is shared with the player.
- **Swedish** — `src/locales/sv/<area>.json`, flat `{ "English": "Svenska" }`.
  The classic console's catalog is loaded underneath
  (`internal/server/web/locales/web/sv.json`), so reusing a classic
  string's exact English text gets its translation for free. Every new
  string needs an entry; `t.plural` for counts.
- **Styles** — `src/pages/<area>/<area>.css`, imported by the area's
  index. Only for what the kit lacks; use `--dz-*` tokens, scope under
  `.dz`.

## How a page is built

Everything comes from `src/ui/index.ts`:

- Layout: `Page`, `PageHeader` (title, subtitle, back link, actions,
  badges), `Card` (a titled section; `tone="danger"` for danger zones),
  `Row`, `Facts`, `Stats` + `Stat`, `Tiles` + `Tile`, `Tabs` (links, one
  URL per tab), `Table` (`rowHref` makes rows links), `Pill`, `Empty`,
  `Avatar`, `Loading`, `ErrorBox`.
- Controls: `Button` (`variant="primary" | "ghost" | "danger" | "link"`,
  `size="sm"`, `icon`), plain `<input>`, `<select>`, `<textarea>` inside
  `Field` (label, hint, error). dz.css styles them.
- `Callout` for notes and warnings in a page.
- Dialogs: `Dialog` for forms; `useDialogs().confirm({...})` and
  `useDialogs().password({...})` for yes/no and password-confirmed
  actions (send the result as the `X-Confirm-Password` header, as the
  classic console does).
- `useToast().ok(text)` / `.err(error)` after an action.
- Data: `useLoad(signal => api(), [deps])` → `{ data, error, loading, reload, set }`.
- Time and money: `fmtDateTime`, `fmtWhen`, `fmtAgo`, `fmtDuration`,
  `fmtMoney`, `toDate`, `toLocalInput` (server timestamps come in two
  formats; these read both).
- Live updates: `useEventWatch(eventId, msg => …)` for `/ws/arrange`.
- Page title in the top bar crumb: `useContext(TopBarTitle)?.(name)`
  from `src/topbar.tsx`.
- Session: `useSession()` from `src/session.tsx` (`state.user`, `refresh`).
- Translation: `const t = useT(); t.t("…")`, `t.plural(n, { one, other })`.

## Rules

1. **Same behaviour as the classic screen**, reading its code in
   arrange.html (find the `view…`/`render…` function for the screen).
   Keep every capability, permission check, confirmation and edge case;
   improve the UX where it is clearly better, never drop a feature. Known
   UX issues from the review are fair game to fix.
2. **No new dependencies.** lucide-react for icons; maplibre-gl is there
   for maps (see `src/designer/ZoneMap.tsx`).
3. **Accessible by default**: real buttons and links, labels on every
   control, focus into dialogs, `aria-live` via the kit's toasts.
4. **Mobile**: every page works at 390px.
5. **Swedish for every string** you add.
6. Match the existing code style; minimal comments, only where the why
   isn't obvious.
