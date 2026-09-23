/* Zonventure player service worker.
 *
 * Hand-written, not generated, because what it must NOT do matters as
 * much as what it does:
 *
 *   - It never caches or answers anything under /api/ or /ws/. Game
 *     state is per team and changes by the second; a cached copy is at
 *     best stale and at worst another team's (a shared device, a second
 *     magic link). Those requests go straight to the network, and the
 *     app's outbox — not this worker — handles being offline.
 *   - It only handles its own scope, /app/play/.
 *
 * One exception to "never answers game data": /media/{id}, the pictures
 * and sounds of the adventure. The page tells the worker which team is
 * playing and which files that team may fetch; the worker downloads them
 * into a cache named for that team and serves only from it. A second
 * team signing in on the same phone gets its own cache and never sees
 * the first team's files, and signing out deletes them.
 *
 * What it does: keeps the app shell openable with no signal. The shell
 * (index.html) is fetched network-first so a deploy reaches players on
 * their next load, falling back to the cached copy offline. Hashed
 * assets are cache-first — their URLs change whenever their bytes do —
 * and assets the current shell no longer references are pruned, so the
 * cache holds one build, not every build ever shipped.
 *
 * "References" follows the shell's stylesheets one level down, because
 * the fonts live there: the brand face is a hashed file only the CSS
 * names. Those are fetched ahead of need, so a player who has never
 * seen an å still gets the latin-ext subset in a dead spot.
 */

const CACHE = "zv-play-shell-v2";
const SCOPE = "/app/play/";
const SHELL = SCOPE;
const MEDIA_PREFIX = "zv-play-media-";
const META = "zv-play-meta";
// Defence in depth against a runaway list: the server already bounds it.
const MAX_PREFETCH = 200;

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((c) => c.add(new Request(SHELL, { cache: "reload" })))
      .catch(() => {
        /* offline at install: the first online load fills the cache */
      }),
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith("zv-play-") && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("message", (event) => {
  const d = event.data || {};
  if (d.type === "prefetch" && Number.isFinite(d.team) && Array.isArray(d.urls)) {
    event.waitUntil(prefetch(d.team, d.urls));
  } else if (d.type === "forget") {
    event.waitUntil(forget(null));
  }
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin === self.location.origin && url.pathname.startsWith("/media/")) {
    event.respondWith(mediaFirst(req));
    return;
  }
  if (url.origin !== self.location.origin || !url.pathname.startsWith(SCOPE)) return;

  if (url.pathname.startsWith(SCOPE + "assets/")) {
    event.respondWith(cacheFirst(req));
    return;
  }
  if (req.mode === "navigate") {
    event.respondWith(shell(req));
  }
});

async function cacheFirst(req) {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok) cache.put(req, res.clone());
  return res;
}

async function shell(req) {
  const cache = await caches.open(CACHE);
  try {
    const res = await fetch(req);
    if (res.ok) {
      // Both copies are taken now: once res is handed to the page its
      // body is consumed, and cloning it later would throw.
      const forCache = res.clone();
      const forScan = res.clone();
      forScan.text().then((html) => keepOnly(cache, html, forCache));
    }
    return res;
  } catch (err) {
    const hit = await cache.match(SHELL);
    if (hit) return hit;
    throw err;
  }
}

// Store the new shell, make sure everything it needs is cached, and drop
// cached assets it no longer references.
async function keepOnly(cache, html, res) {
  await cache.put(SHELL, res);
  const wanted = refs(html, /(?:src|href)="(\/app\/play\/assets\/[^"]+)"/g);
  for (const p of [...wanted]) {
    if (!p.endsWith(".css")) continue;
    const css = await cached(cache, p);
    if (css) for (const u of refs(await css.text(), /url\((\/app\/play\/assets\/[^)"']+)\)/g)) wanted.add(u);
  }
  await Promise.all([...wanted].map((p) => cached(cache, p)));
  for (const req of await cache.keys()) {
    const p = new URL(req.url).pathname;
    if (p.startsWith(SCOPE + "assets/") && !wanted.has(p)) await cache.delete(req);
  }
}

function refs(text, re) {
  const out = new Set();
  for (const m of text.matchAll(re)) out.add(m[1]);
  return out;
}

// The cached response for an asset, fetching and storing it if absent.
async function cached(cache, path) {
  const hit = await cache.match(path);
  if (hit) return hit;
  try {
    const res = await fetch(path);
    if (!res.ok) return null;
    await cache.put(path, res.clone());
    return res;
  } catch {
    return null;
  }
}

// --- media ---------------------------------------------------------------

async function activeTeam() {
  const hit = await (await caches.open(META)).match("/team");
  return hit ? Number(await hit.text()) : null;
}

async function prefetch(team, urls) {
  if ((await activeTeam()) !== team) {
    await forget(team);
    await (await caches.open(META)).put("/team", new Response(String(team)));
  }
  const cache = await caches.open(MEDIA_PREFIX + team);
  const queue = urls.filter((u) => typeof u === "string" && u.startsWith("/media/")).slice(0, MAX_PREFETCH);
  // A few at a time: a fresh join must not fire the whole list at once.
  const worker = async () => {
    while (queue.length) {
      const u = queue.shift();
      if (await cache.match(u)) continue;
      try {
        const res = await fetch(u, { credentials: "same-origin" });
        if (res.ok && res.type === "basic") await cache.put(u, res);
      } catch {
        /* offline or refused: the next prefetch tries again */
      }
    }
  };
  await Promise.all([worker(), worker(), worker(), worker()]);
}

// Delete every team's media except keep's (null deletes all).
async function forget(keep) {
  for (const k of await caches.keys()) {
    if (k.startsWith(MEDIA_PREFIX) && k !== MEDIA_PREFIX + keep) await caches.delete(k);
  }
  if (keep === null) await caches.delete(META);
}

async function mediaFirst(req) {
  const team = await activeTeam();
  if (team === null) return fetch(req);
  const cache = await caches.open(MEDIA_PREFIX + team);
  const hit = await cache.match(req.url);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok && res.type === "basic") cache.put(req.url, res.clone());
  return res;
}
