/* Topbar JS — two responsibilities:
 *   1. Scroll-fade. Toggles `.at-top` on the topbar so the CSS can
 *      fade the glass background in once the user starts scrolling.
 *      Only runs on pages that opt in via `<body class="has-hero">`.
 *      Pages without a dark hero (e.g. /manual) keep the topbar's
 *      light glass bar throughout.
 *   2. Theme. Reads `localStorage.zv-theme` and applies it as a
 *      `data-theme` attribute on <html>. Renders a small toggle
 *      button next to the language picker (at the start of the nav
 *      on pages without one: privacy, terms) that cycles
 *      Light → Dark → Auto (= follow system). The same key is
 *      shared with /play and /arrange so a visitor's choice
 *      round-trips across the whole product.
 *
 * Both pieces run after `defer` so the DOM is parsed; no
 * synchronous render dependencies inside the page body.
 */

(function applyStoredTheme() {
  try {
    var saved = localStorage.getItem('zv-theme');
    if (saved === 'light' || saved === 'dark') {
      document.documentElement.setAttribute('data-theme', saved);
    }
  } catch (e) { /* private mode, etc — defaults still apply */ }
})();

(function scrollFade() {
  var bar = document.querySelector('.topbar');
  if (!bar) return;
  if (!document.body.classList.contains('has-hero')) return;
  var update = function () {
    bar.classList.toggle('at-top', window.scrollY <= 4);
  };
  update();
  window.addEventListener('scroll', update, { passive: true });
})();

(function mountThemeToggle() {
  // Insert the toggle button next to the language picker on every
  // page that has the shared topbar markup. Cycles through three
  // states: light → dark → auto (no attribute, follows OS) → light.
  // Uses the sun / moon glyphs already loaded by the page chrome.
  var picker = document.querySelector('.lang-picker');
  var nav = document.querySelector('.topbar nav');
  if (!picker && !nav) return;
  var btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'theme-toggle';
  btn.setAttribute('aria-label', 'Theme');
  btn.title = 'Theme';
  var iconSun = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>';
  var iconMoon = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
  var iconAuto = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/></svg>';

  function current() {
    return document.documentElement.getAttribute('data-theme');
  }
  function repaint() {
    var v = current();
    if (v === 'dark') { btn.innerHTML = iconMoon; btn.title = 'Theme: dark'; }
    else if (v === 'light') { btn.innerHTML = iconSun; btn.title = 'Theme: light'; }
    else { btn.innerHTML = iconAuto; btn.title = 'Theme: auto (follow system)'; }
  }
  function next() {
    var v = current();
    var n = v === 'light' ? 'dark' : v === 'dark' ? 'auto' : 'light';
    if (n === 'auto') {
      try { localStorage.removeItem('zv-theme'); } catch (e) {}
      document.documentElement.removeAttribute('data-theme');
    } else {
      try { localStorage.setItem('zv-theme', n); } catch (e) {}
      document.documentElement.setAttribute('data-theme', n);
    }
    repaint();
  }
  btn.addEventListener('click', next);
  repaint();
  // Place after the language picker on the topbar.
  if (picker) picker.parentNode.insertBefore(btn, picker.nextSibling);
  else nav.insertBefore(btn, nav.firstChild);
})();
