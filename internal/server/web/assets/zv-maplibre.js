// MapLibre for the server-rendered pages (store, recap, live view), from
// this server's own copy in /assets/maplibre/, which `make web` takes from
// the same version the arrange app bundles. Pages load it with
// import('/assets/zv-maplibre.js') once their content is on screen.
const BASE = '/assets/maplibre/';
let loading = null;

// loadMapLibre resolves to the MapLibre module, its worker and stylesheet
// in place. A failure is forgotten, so the next map tries again.
export function loadMapLibre() {
  loading ??= import(BASE + 'maplibre-gl.mjs').then((m) => {
    const ml = m.default || m;
    ml.setWorkerUrl(new URL(BASE + 'maplibre-gl-worker.mjs', location.href).href);
    if (!document.querySelector('link[data-maplibre]')) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = BASE + 'maplibre-gl.css';
      link.dataset.maplibre = '';
      document.head.append(link);
    }
    return ml;
  }, (e) => {
    loading = null;
    throw e;
  });
  return loading;
}

// mapStyle is the operator's vector style (window.ZV_MAP.style_url, from
// /assets/map-config.js); without one, a raster style of its tiles, or
// OpenFreeMap's style.
export function mapStyle() {
  const c = window.ZV_MAP || {};
  if (c.style_url) return c.style_url;
  if (!c.tile_url) return 'https://tiles.openfreemap.org/styles/liberty';
  return {
    version: 8,
    sources: {
      osm: {
        type: 'raster',
        tiles: [c.tile_url],
        tileSize: 256,
        maxzoom: 19,
        attribution: c.attribution || '© OpenStreetMap contributors',
      },
    },
    layers: [{ id: 'osm', type: 'raster', source: 'osm' }],
  };
}

// gestureLocale words the hints a cooperative map shows: how to zoom with
// the wheel (`wheel`, with {key} for Ctrl or ⌘) and how to pan on a phone.
export function gestureLocale(wheel, touch) {
  return {
    'CooperativeGesturesHandler.WindowsHelpText': wheel.replace('{key}', 'Ctrl'),
    'CooperativeGesturesHandler.MacHelpText': wheel.replace('{key}', '⌘'),
    'CooperativeGesturesHandler.MobileHelpText': touch,
  };
}
