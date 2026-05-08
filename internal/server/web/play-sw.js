// Player-side service worker. Powers the offline experience by
// caching the play app shell + every adventure asset the player's
// team will need, so puzzles render even in a dead spot.
//
// Three responsibilities:
//   1. Install: cache the play page + i18n + the few static assets
//      the page pulls in. The shell is what makes the app *load*
//      offline at all.
//   2. Message handler: when the page sends {type:'prefetch', team_id, urls},
//      cache.addAll() those URLs into the team's asset cache. Per-team
//      partitioning matters: without it, Team A's prefetched bytes
//      would serve cache-first to Team B on a shared device and
//      bypass the server-side canViewPageImage gate (which is also
//      what the adventures.strict_assets column gates server-side).
//   3. Fetch handler: cache-first for /adventure-assets/* (immutable
//      blobs keyed by id, so a cache hit is always correct, AND the
//      cache is keyed by team) — network-first with cache fallback
//      for the app shell.
//
// Cache version is the build version the server injects at serve time
// (see /play-sw.js handler in internal/server/server.go). Each deploy
// rotates the cache key, so activate's purge cleans up old caches
// without anyone bumping a constant by hand. "dev" is the fallback for
// unversioned local builds.

const BUILD_VERSION = '__ZV_BUILD_VERSION__';
const SHELL_CACHE = 'zv-play-shell-' + BUILD_VERSION;
const ASSET_PREFIX = 'zv-play-assets-';
const ASSET_SUFFIX = '-' + BUILD_VERSION;

// activeTeamID is mirrored into a sentinel cache entry below so it
// survives SW restarts. The page re-sends {type: 'prefetch'} on every
// load so this is also refreshed at session start, but the persistent
// copy keeps the fetch handler honest if the SW wakes for a fetch
// before the page's prefetch message arrives.
let activeTeamID = null;
const SLOT_SENTINEL = '/__zv_active_team__';

// Files to pre-cache on install. The play page itself is fetched
// network-first so a fresh deploy reaches users; we still cache it on
// the way through so an offline reload renders. /i18n.js is only
// strictly needed for translation, but caching it ensures the UI
// strings render in the player's language even offline.
const SHELL_URLS = [
  '/play',
  '/i18n.js',
  '/favicon.svg',
];

function teamCacheName(teamID) {
  return ASSET_PREFIX + String(teamID) + ASSET_SUFFIX;
}

// loadActiveTeamID reads the sentinel cache entry the message handler
// writes on every prefetch. Lets the fetch handler recover the active
// team on SW restart without waiting for the page to message us first.
async function loadActiveTeamID() {
  if (activeTeamID != null) return activeTeamID;
  try {
    const cache = await caches.open(SHELL_CACHE);
    const hit = await cache.match(SLOT_SENTINEL);
    if (!hit) return null;
    const body = await hit.json();
    if (body && Number.isFinite(body.team_id)) {
      activeTeamID = body.team_id;
      return activeTeamID;
    }
  } catch (_) { /* fall through */ }
  return null;
}

async function setActiveTeamID(teamID) {
  if (!Number.isFinite(teamID)) return;
  if (activeTeamID === teamID) return;
  activeTeamID = teamID;
  // Persist for the next SW wake-up.
  try {
    const cache = await caches.open(SHELL_CACHE);
    await cache.put(SLOT_SENTINEL,
      new Response(JSON.stringify({ team_id: teamID }), {
        headers: { 'Content-Type': 'application/json' },
      }));
  } catch (_) { /* best-effort */ }
  // Sweep any per-team asset caches that don't match the new active
  // team. A shared device that swaps between teams should not retain
  // the previous team's prefetched bytes — that's the whole point of
  // partitioning the cache here.
  try {
    const names = await caches.keys();
    const keep = teamCacheName(teamID);
    await Promise.all(names
      .filter((n) => n.startsWith(ASSET_PREFIX) && n !== keep)
      .map((n) => caches.delete(n)));
  } catch (_) { /* best-effort */ }
}

self.addEventListener('install', (ev) => {
  ev.waitUntil((async () => {
    const cache = await caches.open(SHELL_CACHE);
    // addAll fails atomically; if any URL 4xx/5xxs we'd lose the
    // whole shell prime. Each URL must be reachable in the install
    // path of a freshly registered worker, so partial failures here
    // would mean a real bug worth surfacing — let it throw.
    await cache.addAll(SHELL_URLS);
    self.skipWaiting();
  })());
});

self.addEventListener('activate', (ev) => {
  ev.waitUntil((async () => {
    // Sweep stale caches: any zv-play-shell-* whose version doesn't
    // match the current build, plus any zv-play-assets-{id}-* cache
    // whose suffix doesn't match the current ASSET_SUFFIX. Tying the
    // suffix to the build version makes the asset purge happen
    // automatically on each deploy without an operator-tunable knob.
    // Caches outside the "zv-play-" namespace are left alone — other
    // SWs (none today) might own them.
    const names = await caches.keys();
    await Promise.all(names
      .filter((n) => {
        if (n === SHELL_CACHE) return false;
        if (n.startsWith('zv-play-shell-')) return true;
        if (n.startsWith(ASSET_PREFIX)) return !n.endsWith(ASSET_SUFFIX);
        return false;
      })
      .map((n) => caches.delete(n)));
    // Deliberately NOT calling clients.claim(): a hot-swap of the
    // controller mid-event would yank a player's offline state out
    // from under them with no rollback. Existing tabs keep their
    // current SW until natural reload; new tabs land on the fresh
    // worker. skipWaiting in install ensures the new worker is
    // ready when the next reload happens.
  })());
});

self.addEventListener('message', (ev) => {
  const data = ev.data || {};
  if (data.type === 'prefetch' && Array.isArray(data.urls)) {
    ev.waitUntil((async () => {
      if (Number.isFinite(data.team_id)) await setActiveTeamID(data.team_id);
      const teamID = await loadActiveTeamID();
      if (teamID == null) return; // no slot context — refuse to cache
      await prefetchAssets(teamCacheName(teamID), data.urls);
    })());
  } else if (data.type === 'skipWaiting') {
    self.skipWaiting();
  }
});

// prefetchAssets fetches each URL one at a time and stores the OK
// response in the asset cache. We intentionally don't use
// cache.addAll because that aborts the entire batch on the first
// failure — a hint asset that 403s under the reveal gate is normal,
// not a hard error. opaque/redirected responses are skipped because
// the Cache API stores them but they re-serve as `network-error`
// next time which is worse than a cache miss.
// MAX_PREFETCH_URLS caps the number of URLs the SW will pull on one
// prefetch message. A misconfigured manifest, a malicious arranger
// pointing at an unbounded asset list, or a forgotten image-loop
// won't drain the team's data plan / device storage. The server's
// /api/player/manifest is the source of truth and already bounds the
// page count; this is defence in depth.
const MAX_PREFETCH_URLS = 200;

async function prefetchAssets(cacheName, urls) {
  const cache = await caches.open(cacheName);
  if (urls.length > MAX_PREFETCH_URLS) {
    urls = urls.slice(0, MAX_PREFETCH_URLS);
  }
  // Limit concurrency: a fresh team-join shouldn't fire 200 parallel
  // requests at the server; trickle them six at a time.
  const queue = urls.slice();
  async function worker() {
    while (queue.length) {
      const url = queue.shift();
      try {
        const res = await fetch(url, { credentials: 'same-origin' });
        if (res.ok && res.type === 'basic') {
          await cache.put(url, res.clone());
        }
      } catch (_) { /* network error — skip this one */ }
    }
  }
  await Promise.all([worker(), worker(), worker(), worker(), worker(), worker()]);
}

// HANDLED_PATHS is the explicit allowlist of URL prefixes / exact
// paths this worker is allowed to intercept. Registration scope is
// `/` (we need to cache /adventure-assets/* and /org-logo/* which
// are siblings of /play, not children), but defence-in-depth: any
// path not in this list passes straight through to the network so a
// future bug in one of the handlers below can't accidentally widen
// the SW's reach into /arrange, /api/admin/*, etc.
const HANDLED_PREFIXES = ['/adventure-assets/', '/org-logo/'];
const HANDLED_EXACT = new Set(['/play', '/i18n.js', '/favicon.svg']);

self.addEventListener('fetch', (ev) => {
  const req = ev.request;
  // Only handle same-origin GETs. POSTs (answer submit, etc.) flow
  // through the page's offline action queue, not the SW.
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  // Explicit early-return: only intercept the player-related paths.
  // Everything else (e.g. /arrange, /api/admin/*, /store) is the
  // browser's responsibility, even though our scope nominally covers
  // them.
  const exact = HANDLED_EXACT.has(url.pathname);
  const prefixed = HANDLED_PREFIXES.some((p) => url.pathname.startsWith(p));
  if (!exact && !prefixed) return;

  if (url.pathname.startsWith('/adventure-assets/')) {
    ev.respondWith(teamScopedCacheFirst(req));
    return;
  }
  // Org brand logo — cache against the active team so a shared
  // device doesn't serve org A's logo to org B's player. Bytes are
  // bound to a numeric org id so the URL is stable across re-uploads
  // (the cache-bust on upload is handled at the arranger console
  // layer, separate from the player's offline store).
  if (url.pathname.startsWith('/org-logo/')) {
    ev.respondWith(teamScopedCacheFirst(req));
    return;
  }
  // /play, /i18n.js, /favicon.svg — the shell.
  ev.respondWith(networkFirst(req, SHELL_CACHE));
});

// teamScopedCacheFirst routes the request through the active team's
// asset cache. If we don't yet know which team the client belongs to
// (very first paint, or a SW that hasn't seen a prefetch message
// since a cold restart), we pass the request straight to the network
// without caching — the server still enforces canViewPageImage, so
// nothing leaks; the device just won't have offline assets until the
// page next runs registerOfflineCache.
async function teamScopedCacheFirst(req) {
  const teamID = await loadActiveTeamID();
  if (teamID == null) {
    return fetch(req);
  }
  return cacheFirst(req, teamCacheName(teamID));
}

async function cacheFirst(req, cacheName) {
  const cache = await caches.open(cacheName);
  const hit = await cache.match(req);
  if (hit) return hit;
  try {
    const res = await fetch(req);
    if (res.ok && res.type === 'basic') {
      cache.put(req, res.clone());
    }
    return res;
  } catch (err) {
    // Offline + uncached — let the page handle the failure (broken
    // image / audio element). Returning a Response with status 504
    // would force every <img> into onerror; better to surface the
    // genuine network error so devtools shows what's missing.
    throw err;
  }
}

async function networkFirst(req, cacheName) {
  const cache = await caches.open(cacheName);
  try {
    const res = await fetch(req);
    if (res.ok && res.type === 'basic') {
      cache.put(req, res.clone());
    }
    return res;
  } catch (err) {
    const hit = await cache.match(req);
    if (hit) return hit;
    throw err;
  }
}
