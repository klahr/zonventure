# MCP connector

## Status

**Shipped**: HTTP MCP server at `POST /mcp`, per-user bearer tokens
minted from settings, tools for authoring (lanes and graphs), running
events and watching them — `tools/list` is the authoritative list.
Off by default — operators opt in via `ZV_MCP_ENABLED=1`.

**Sign-in**: a pasted `zvmcp_…` token, or OAuth 2.1 — what claude.ai
custom connectors, and so the Claude mobile app, use. See
[Connecting with OAuth](#connecting-with-oauth).

**Not yet shipped**: SSE streaming of tool output.

## What it is

The Model Context Protocol ([modelcontextprotocol.io](https://modelcontextprotocol.io))
lets an LLM client (Claude desktop, claude.ai connectors, Cursor,
…) call a server's tools as part of its reasoning loop. zv exposes
adventure authoring as MCP tools so an arranger can say

> "Make me a 6-puzzle forest hunt in Hagaparken with two shuffled
> lanes — first lane is the kid-friendly intro, second lane is the
> hard ones."

and the model drafts → validates → commits the adventure against
their org, the same way clicking through `/arrange` would. Heavy
content (the actual puzzle prompts, accept-list answers, lane
plan, GPS coordinates) is the model's job; zv just owns the
schema, validation, and persistence.

## Design

### Transport

Single endpoint, `POST /mcp`. JSON-RPC 2.0 over HTTP — no SSE,
no streaming, no per-connection state. Tool calls are
request/response: the client POSTs `tools/call`, the server runs
the tool synchronously, returns one JSON envelope. Plenty for
authoring, where every tool finishes in milliseconds.

### Auth

Bearer token in `Authorization`. Tokens are minted by individual
users from **Settings → Account → API tokens**, stored as
`sha256(plaintext)` in the `mcp_tokens` table, and resolved at
request time into the `auth.User` that minted them. The MCP
session inherits that user's authoring policy verbatim — same org
scoping, same `canAuthor` gate that distinguishes `org_admin`
(may rewrite content) from `org_member` (may run events but not
edit puzzles).

Each tool also has an audience. A global admin's token sees and
calls only the operator tools (`grant_slots_to_event`,
`list_purchases`, `get_purchase`, `describe_event_lifecycle`):
the admin curates and monitors, it doesn't author or run events.
Org tools refuse it in-band with `forbidden_audience`. Catalog
listing (`published`) is the operator's call, so
`update_adventure_meta` refuses it too.

Listing review has its own audience: the tools `list_listing_reviews`,
`get_listing_review`, `approve_listing` and `reject_listing` answer
only users a platform admin made listing reviewers (Admin → Listing
reviews), whatever their role. Give an AI reviewer an account of its
own with only that flag, and mint its token there: it can review and
nothing else, never its own organisation's listings, and every decision
it makes is logged as an assistant's for spot-checks. Organisation
users submit with `submit_listing_for_review`, passing `attest: true`
only once the user has confirmed the content follows the terms.

The same tokens also authenticate the REST API under
`/api/admin`, whether or not `ZV_MCP_ENABLED` is set. Send
`Authorization: Bearer zvmcp_…`; no session cookie or CSRF header is
needed. A `read` token is limited to GET/HEAD; anything else needs
`write`. Account and credential routes (token management, password,
email, TOTP, sessions, account deletion, data export, billing portal)
answer 403 to a token so a leaked one can't mint more or take over
the account.

The plaintext token is shown **once** at creation and never again;
the settings UI keeps a `zvmcp_AB…` prefix per row so the user can
identify which token to revoke without us being able to re-reveal
the secret.

### Tool surface

Everything an author does on the website can be done from a chat — on
a phone too: make an adventure, build and change its steps, give it a
look, an icon and pictures, publish it, and create, fill and run its
events. `describe_event_lifecycle` returns the full tool index by
phase; `TestLifecycleIndexesEveryTool` keeps it in step with the registered tools.

| Phase | Tools |
|-------|-------|
| Adventures | `list_adventures` (can_edit, playable) · `get_adventure` (settings, brief, look, icon) · `create_adventure` (a start → end draft and an optional brief) · `update_adventure_meta` · `set_adventure_icon` / `remove_adventure_icon` · `list_org_themes` · `delete_adventure` |
| Steps | `get_authoring_guide` · `get_adventure_graph` · `get_step` · `edit_adventure_graph` · `undo_adventure_graph_edit` · `save_adventure_graph` · `validate_adventure_graph` · `publish_adventure_graph` · `get_creator_position` |
| Files | `get_upload_link` · `list_adventure_media` · `upload_adventure_media` |
| Trying it | `create_dev_ticket` · `get_dev_screen` · `dev_go_to` · `dev_reset` · `simulate_adventure` · `playtest_start` / `playtest_act` / `playtest_end` |
| Co-authors | `list_adventure_threads` · `comment_on_adventure` · `suggest_step_change` · `resolve_adventure_thread` · `set_step_status` |
| Events | `create_event` · `update_event` · `start_event` · `pause_event` · `resume_event` · `end_event` · `cancel_event` · teams, players, invitations, messages, watching and results |

`edit_adventure_graph` takes a batch of ops, all-or-nothing:
`put_node` / `delete_node`, `set_text` (change words by quoting them)
and `patch_node` (a merge patch; blocks and matchers merge by id,
outputs by port), `put_zone` (`here: true` centres it on the author) /
`delete_zone`, `connect` / `disconnect`, `put_theme` / `delete_theme`,
and `set_option` (adventure-wide switches: `hide_score`,
`journal_by_unlock`, `quiz_puzzles`).
Without a step, `set_text` and `patch_node` edit the one the author's
dev phone shows.

**Pictures from a phone.** The model can neither send a photo's bytes
nor run curl there, so `get_upload_link` returns a link for the chat:
the user taps it and picks the file on the page it opens. A link adds
a step's picture, sound or video (`list_adventure_media` then gives
its id), sets the adventure's icon (`for: icon`), or an event's cover
(`for: event_cover`). With a shell, `curl -F file=@photo.jpg <link>`
does the same.

**Events.** A self-guided adventure (`public_for_sale: true`) on the
organisation's own adventure is created, priced with `update_event`
and started. A scheduled event needs an event pass; `create_event`
says where to buy one on the website, unless the server runs
simulated payments, where `create_purchase` + `confirm_purchase` buy it.

Two prompts build on them: `review_adventure` leaves a playtester's notes as threads, and `pick_up_tasks` works through what the authors asked the assistant for.

The author doesn't have to explain any of this. The server sends
usage instructions in its `initialize` reply, which clients put in the
model's context; offers a `field_session` prompt (in Claude Code:
`/mcp__zv__field_session [slug]`) that starts a session; and
`get_authoring_guide` returns step recipes as exact node JSON, checked
by `guide_test.go` against the parser and validator. The text lives in
`internal/mcp/guide.go`.

The position comes from **Field mode** (`/app/arrange/#/field`), which the
author keeps open on their phone: it reports GPS to
`PUT /api/admin/me/position`, keeps the screen awake, and the server
stores only the latest fix.

## Setup — users

1. Sign into `/arrange`, open **Settings → Account**, scroll to
   **API tokens**.
2. Click **New token**, name it (e.g. `claude-desktop`), copy the
   shown `zvmcp_…` plaintext. **The token is shown once** — paste
   it into your password manager before dismissing the modal.
3. In your MCP client, register a remote server that POSTs to
   `https://<your-host>/mcp` with header
   `Authorization: Bearer zvmcp_…`. The exact config shape varies
   by client (Claude desktop's `claude_desktop_config.json`,
   claude.ai's connector picker, Cursor's settings, …) — consult
   the client's MCP docs for the JSON layout. The two values you
   need are the URL and the Authorization header.
4. Restart the client and ask it to list your adventures — it
   should call `list_adventures` and read back what's there.

Quick smoke test from the command line:

```sh
curl -s -X POST https://your-host/mcp \
  -H "Authorization: Bearer zvmcp_…" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Should return a JSON-RPC response listing the tools.

To revoke a token (laptop lost, contractor offboarded, you minted
it for a one-shot script and don't need it any more), delete the
row from the same settings card. Any client still using the token
gets a 401 on the next call.

## Connecting with OAuth

With OAuth a client signs you in; nothing is pasted. It is on when
`ZV_MCP_ENABLED=1` and `ZV_PUBLIC_URL` is the server's real https
origin, which becomes the OAuth issuer.

1. In claude.ai: **Settings → Connectors → Add custom connector**, URL
   `https://<your-host>/mcp`. (Claude Code: `claude mcp add --transport
   http zv https://<your-host>/mcp` with no header, then `/mcp` →
   Authenticate.)
2. The browser opens Zonventure. Sign in as usual — password, TOTP or
   single sign-on — and **Allow** the client. Someone in several
   organizations picks the one it acts in; the token works there only,
   and stops if they leave it. Connect again for another.
3. The connector is then available in the Claude apps signed in to the
   same account, the mobile app included.

The approval is an ordinary API token, "Claude (signed in)", under
**Settings → Account → API tokens**; deleting it disconnects the
client. Its access token lasts an hour and is renewed with a refresh
token that rotates on each use and lapses after 30 idle days; replaying
a replaced refresh token revokes the approval.

How it works (`internal/oauth`): the `/mcp` 401 names
`/.well-known/oauth-protected-resource`, which points at this server's
metadata (`/.well-known/oauth-authorization-server`). The client
registers itself at `/oauth/register` (public clients only, redirect
URIs on the allowlist — see `ZV_OAUTH_REDIRECT_HOSTS` in
selfhosting.md), sends the user to `/oauth/authorize` with PKCE S256,
which forwards to the console's consent page `#/authorize`, and
exchanges the code at `/oauth/token`.

## Setup — operators

The endpoint is **off by default**. To enable:

```sh
# .env
ZV_MCP_ENABLED=1
```

`docker compose up -d` and the route lights up. No DB migration
beyond the standard zv migration set; `mcp_tokens` ships in
migration `0045`.

For self-host operators behind a reverse proxy, allow `POST /mcp`
with no body-size shenanigans (the server caps at 1 MiB in code).
Long-lived connections are not required — each tool call is a
single short-lived request.

## Security posture

- **Token storage** — sha256 only. A DB compromise leaks the
  ability to mint adventures for the affected users (which session
  theft already offers via the cookie table) but **not** anything
  reusable against external systems if the user happened to also
  paste the token elsewhere.
- **Per-user scope** — every tool call resolves to the minting
  user's `auth.User`, runs through the same `canAuthor` /
  `staffCanSeeAdventure` gates `/arrange` uses. There is no global "MCP
  superuser" credential.
- **No password-confirm bypass** — the regular `DELETE /api/admin/adventures/{slug}`
  HTTP path requires a fresh password confirmation; the MCP
  `delete_adventure` tool replaces that gate with the bearer
  token *plus* an explicit `confirm:true` two-phase flow. The
  rationale: the token IS the credential, and a re-prompt-for-
  password UX would be impossible to drive from inside an LLM
  conversation. Re-evaluate if you suspect token theft is more
  likely than session theft for your deployment.
- **Files go through upload links** — a link works for one
  adventure (or event), as the user who asked for it, for 30 minutes
  and 20 files, under the server's per-file caps. Base64 through
  `upload_adventure_media` / `set_adventure_icon` is for small files.
- **Rate-limited at the proxy layer** — zv itself doesn't apply a
  per-token rate limit (yet). If you expose the endpoint to a
  large pool of users, front it with a proxy that does.

## Troubleshooting

| Symptom                                              | Likely cause                                                                                  |
|------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| `404` on `POST /mcp`                                 | `ZV_MCP_ENABLED` not set; route is unmounted.                                                 |
| `401` on every call                                  | Token revoked, or pasted with stray whitespace / missing `Bearer ` prefix in the header.      |
| `forbidden: you cannot edit this adventure`          | Caller is `org_member` (race director). Authoring requires `org_admin` of the owning org.     |
| `forbidden_audience: tool … is not available …`      | The token belongs to the global admin, who can't author or run events. Mint one as an org user. |
| `slug may only contain a-z 0-9 and hyphens`          | The model picked a slug with whitespace, capitals, or punctuation. |
| `This upload link is unknown, used up or expired`    | Upload links last 30 minutes and 20 files; ask for a new one. |

Server logs everything as structured `mcp.*` events (`mcp.auth.lookup`,
`mcp.encode_response`, …) so a tail with `grep mcp\\.` is the
fastest place to look.

## Internals

- `internal/mcp/` — protocol layer (`mcp.go`), HTTP dispatch +
  bearer auth (`server.go`), tool registry + handlers (`tools.go`).
- `internal/admin/upload_links.go` — upload links and their page.
- `internal/admin/mcp_tokens.go` — token CRUD handlers + the
  `LookupMCPToken` resolver the MCP server calls.
- `internal/db/migrations/0045_mcp_tokens.sql` — the table.

Adding a new tool: register a `ToolDescriptor` + handler in
`internal/mcp/tools.go`, expose any new admin entry point as a
plain `func(ctx, db, …)` (mirroring `AddPageFromExport` or
`SetLaneMode`) so it's reachable without an HTTP request, and
call it from the handler. No protocol-level work needed; the
JSON-RPC dispatch is generic.
