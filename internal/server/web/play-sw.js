// The classic player's service worker, retired. Phones that installed
// it at the site root fetch this on their next update check: it clears
// the caches it made and unregisters itself, so nothing at the root
// intercepts requests any more. The player app has its own worker,
// scoped to /app/play/.

self.addEventListener('install', () => self.skipWaiting());

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k.startsWith('zv-play-')).map((k) => caches.delete(k)));
    await self.registration.unregister();
  })());
});
