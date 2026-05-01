// Player-side service worker. Powers the offline experience by
// caching the play app shell + every adventure asset the player's
// team will need, so puzzles render even in a dead spot.
//
// Three responsibilities:
//   1. Install: cache the play page + i18n + the few static assets
//      the page pulls in. The shell is what makes the app *load*
//      offline at all.
//   2. Message handler: when the page sends {type:'prefetch', urls},
//      cache.addAll() those URLs (puzzle images, audio, hint media).
//      Individual failures (a 403 from a reveal-gated hint asset, a
//      transient 5xx) are swallowed so the whole prefetch doesn't
//      abort on one missing item.
//   3. Fetch handler: cache-first for /adventure-assets/* (immutable
//      blobs keyed by id, so a cache hit is always correct); network
//      -first with cache fallback for the app shell (so a fresh
//      deploy reaches users while still leaving offline users with
//      *some* working UI).
//
// Cache version is baked into the SW source — bumping the constant
// rotates the cache key, which causes activate's purge of any older
// caches to clean up after a deploy.

const SHELL_CACHE = 'zv-play-shell-v1';
const ASSET_CACHE = 'zv-play-assets-v1';

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
    // Sweep older caches when the version constant rolls forward.
    const keep = new Set([SHELL_CACHE, ASSET_CACHE]);
    const names = await caches.keys();
    await Promise.all(names.filter((n) => !keep.has(n)).map((n) => caches.delete(n)));
    await self.clients.claim();
  })());
});

self.addEventListener('message', (ev) => {
  const data = ev.data || {};
  if (data.type === 'prefetch' && Array.isArray(data.urls)) {
    ev.waitUntil(prefetchAssets(data.urls));
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
async function prefetchAssets(urls) {
  const cache = await caches.open(ASSET_CACHE);
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

self.addEventListener('fetch', (ev) => {
  const req = ev.request;
  // Only handle same-origin GETs. POSTs (answer submit, etc.) flow
  // through the page's offline action queue, not the SW.
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname.startsWith('/adventure-assets/')) {
    ev.respondWith(cacheFirst(req, ASSET_CACHE));
    return;
  }
  // Org brand logo — same cache-first treatment as adventure
  // assets so an offline reload still paints the brand strip.
  // The bytes are bound to a numeric org id so the URL is stable
  // across re-uploads (the cache-bust on upload is handled at the
  // arranger console layer, separate from the player's offline
  // store).
  if (url.pathname.startsWith('/org-logo/')) {
    ev.respondWith(cacheFirst(req, ASSET_CACHE));
    return;
  }
  if (url.pathname === '/play' || url.pathname === '/i18n.js' || url.pathname === '/favicon.svg') {
    ev.respondWith(networkFirst(req, SHELL_CACHE));
    return;
  }
  // Pass-through for everything else (API calls, WS upgrades, etc.).
  // The SW intentionally doesn't second-guess /api/* — those are
  // either idempotent reads (which the page can re-issue on
  // reconnect) or mutations (which go through the offline queue).
});

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
