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
 * What it does: keeps the app shell openable with no signal. The shell
 * (index.html) is fetched network-first so a deploy reaches players on
 * their next load, falling back to the cached copy offline. Hashed
 * assets are cache-first — their URLs change whenever their bytes do —
 * and assets the current shell no longer references are pruned, so the
 * cache holds one build, not every build ever shipped.
 */

const CACHE = "zv-play-shell-v1";
const SCOPE = "/app/play/";
const SHELL = SCOPE;

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

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
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

// Store the new shell and drop cached assets it no longer references.
async function keepOnly(cache, html, res) {
  await cache.put(SHELL, res);
  const wanted = new Set();
  for (const m of html.matchAll(/(?:src|href)="(\/app\/play\/assets\/[^"]+)"/g)) wanted.add(m[1]);
  for (const req of await cache.keys()) {
    const p = new URL(req.url).pathname;
    if (p.startsWith(SCOPE + "assets/") && !wanted.has(p)) await cache.delete(req);
  }
}
