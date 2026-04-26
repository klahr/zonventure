/* Topbar scroll-fade. Tiny script: toggles `.at-top` on the topbar
 * so the CSS can fade the glass background in once the user starts
 * scrolling. Without this script the topbar shows its solid scrolled
 * state always — readable, just not animated. The page content
 * itself is fully rendered server-side so a no-JS / curl / crawler
 * visitor sees everything that matters.
 *
 * Only runs on pages that opt in via `<body class="has-hero">`.
 * Pages without a dark hero (e.g. /manual) need the topbar to stay
 * solid all the way to the top; otherwise the white-on-white text
 * over their light body would be unreadable.
 */
(function () {
  var bar = document.querySelector('.topbar');
  if (!bar) return;
  if (!document.body.classList.contains('has-hero')) return;
  var update = function () {
    bar.classList.toggle('at-top', window.scrollY <= 4);
  };
  update();
  window.addEventListener('scroll', update, { passive: true });
})();
