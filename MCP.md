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

| Tool                    | Kind  | Args                              | Notes                                                     |
|-------------------------|-------|-----------------------------------|-----------------------------------------------------------|
| `list_adventures`       | read  | —                                 | Slugs, titles, lane / puzzle counts visible to caller.    |
| `get_adventure`         | read  | `slug`                            | Full envelope, no asset bytes.                            |
| `validate_adventure`    | dry   | `envelope, slug?`                 | Run the import pipeline against a rolled-back tx.         |
| `create_adventure`      | write | `envelope, slug?`                 | Same JSON the `.zvadv.zip` import accepts.                |
| `add_puzzle`            | write | `slug, page`                      | Append one page; lane by `lane_idx`.                      |
| `update_adventure_meta` | write | `slug, patch`                     | Partial patch — title, colors, intro/outro, language, …   |
| `set_lane_mode`         | write | `slug, lane_idx, mode`            | `"linear"` ↔ `"shuffled"`.                                |
| `delete_adventure`      | write | `slug, confirm?`                  | Two-phase: dry-run summary first, then `confirm:true`.    |

Schema for `envelope` and `page` matches the export/import format
in `internal/admin/portability.go` (`exportAdventureDTO`,
`exportPage`). Binary assets — page images, hint audio, the
adventure avatar — cannot travel through MCP; the model writes
text content + `image_alt` placeholders, and the user attaches
files later in `/arrange`.

### Building an adventure on site

Graph tools for an assistant building an adventure in conversation,
one step at a time, while the author walks the area:

| Tool                        | Kind  | Notes                                                                 |
|-----------------------------|-------|-----------------------------------------------------------------------|
| `create_graph_adventure`    | write | New adventure with a start → end draft and an optional brief.         |
| `update_adventure_meta`     | write | `brief`: theme, audience, length… what every step is designed against. |
| `edit_adventure_graph`      | write | Batch of `put_node` / `delete_node` / `put_zone` / `delete_zone` / `connect` / `disconnect`, all-or-nothing. `put_zone` with `here: true` centres the zone on the author. An optional `note` is shown live to people watching in the designer. |
| `undo_adventure_graph_edit` | write | Restore the draft from before the last `edit_adventure_graph`.         |
| `get_creator_position`      | read  | The author's last reported position and its age.                      |
| `get_media_upload_url`      | write | A 15-minute link for one adventure: `curl -F file=@photo.jpg <url>` uploads without the bytes passing through the model. |
| `upload_adventure_media`    | write | Base64 image/audio/video — small files only, for clients without a shell. |
| `simulate_adventure`        | read  | Play the saved draft through actions with the real rules.             |

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
SELFHOSTING.md), sends the user to `/oauth/authorize` with PKCE S256,
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
- **No asset writes** — `create_adventure` / `add_puzzle` reject
  any manifest that references image / audio bytes. An LLM cannot
  upload binary content via MCP; closes the easy "what if it
  generates 200 MB of nonsense PNG and fills the disk?" path.
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
| `unknown export format: …`                           | The model produced an envelope with a typo'd `format` tag. The wrapper accepts a bare adventure object — drop the wrapper or set `format` to `"zonventure-adventure-v1"`. |
| `image asset "p0_img0.jpg" missing from archive`     | Manifest references binary assets that MCP can't carry. Strip the `images` / `audio` arrays and attach the files in `/arrange` instead. |
| `slug may only contain a-z 0-9 and hyphens`          | The model picked a slug with whitespace, capitals, or punctuation. Patch the manifest's `slug`. |

Server logs everything as structured `mcp.*` events (`mcp.auth.lookup`,
`mcp.encode_response`, …) so a tail with `grep mcp\\.` is the
fastest place to look.

## Internals

- `internal/mcp/` — protocol layer (`mcp.go`), HTTP dispatch +
  bearer auth (`server.go`), tool registry + handlers (`tools.go`).
- `internal/admin/portability.go` — shared `ImportEnvelopeJSON`
  used by both the multipart `.zvadv.zip` import and
  `create_adventure` / `validate_adventure`.
- `internal/admin/mcp_tokens.go` — token CRUD handlers + the
  `LookupMCPToken` resolver the MCP server calls.
- `internal/db/migrations/0045_mcp_tokens.sql` — the table.

Adding a new tool: register a `ToolDescriptor` + handler in
`internal/mcp/tools.go`, expose any new admin entry point as a
plain `func(ctx, db, …)` (mirroring `AddPageFromExport` or
`SetLaneMode`) so it's reachable without an HTTP request, and
call it from the handler. No protocol-level work needed; the
JSON-RPC dispatch is generic.
