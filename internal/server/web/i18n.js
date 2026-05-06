// Shared i18n module for the three web entry points (play, arrange,
// live). Keeping it as a single embedded script means each HTML page
// stays self-contained in its own <script> block but can still call
// t() without duplicating the dictionary per page.
//
// Convention: the key is the English source string. The English dict
// is empty so t("Log in") returns "Log in" when lang=en; Swedish (and
// future languages) override individual strings by exact key. Puzzle
// content (title, prompt, hint, accept list) is author-provided and is
// intentionally NOT translated — an adventure authored in one language
// renders in that language regardless of the UI locale.
//
// Variable interpolation uses {{name}} placeholders: t("Puzzle {{i}}/{{n}}", {i:1,n:3}).
(function () {
  'use strict';

  // DICT is injected at server startup from the per-locale JSON
  // files under internal/server/web/locales/web/<lang>.json. The
  // identifier on the right of the assignment is a substitution
  // marker the Go server replaces with a real JS object before
  // serving — see internal/server/i18n_load.go::renderI18nJS. The
  // marker is intentionally a bare identifier (not a comment) so
  // an unwired build fails noisily at load time rather than
  // silently shipping an empty dictionary.
  const DICT = __ZV_I18N_DICT__;


  // LANG_LABEL maps BCP 47 codes to a { flag, name } pair. Used by
  // two surfaces:
  //   1. makeLangPicker — the UI locale switcher. Only renders
  //      options for codes in `available()` (the keys of DICT, ie
  //      the languages we've translated the chrome into).
  //   2. flagFor — adventure-content language pills on the Catalog,
  //      Adventure detail, and Event detail. Authors can pick any
  //      BCP 47 tag for their puzzle content; we want a recognisable
  //      flag for the common ones, with a graceful empty-string
  //      fallback for tags we don't have an entry for.
  //
  // English uses 🇬🇧 because the product is EU-facing; swap to 🇺🇸 for
  // an Americas launch.
  //
  // The flag is a Regional-Indicator-pair emoji that renders as a
  // country flag on systems with emoji-flag fonts (every modern OS
  // except Windows pre-11; Windows users see the bare letters,
  // which is acceptable as a degradation — the language code is
  // also visible alongside).
  const LANG_LABEL = {
    en:        { flag: '🇬🇧', name: 'English' },
    sv:        { flag: '🇸🇪', name: 'Svenska' },
    no:        { flag: '🇳🇴', name: 'Norsk' },
    nb:        { flag: '🇳🇴', name: 'Norsk bokmål' },
    nn:        { flag: '🇳🇴', name: 'Nynorsk' },
    da:        { flag: '🇩🇰', name: 'Dansk' },
    fi:        { flag: '🇫🇮', name: 'Suomi' },
    is:        { flag: '🇮🇸', name: 'Íslenska' },
    de:        { flag: '🇩🇪', name: 'Deutsch' },
    fr:        { flag: '🇫🇷', name: 'Français' },
    es:        { flag: '🇪🇸', name: 'Español' },
    pt:        { flag: '🇵🇹', name: 'Português' },
    'pt-br':   { flag: '🇧🇷', name: 'Português (Brasil)' },
    it:        { flag: '🇮🇹', name: 'Italiano' },
    nl:        { flag: '🇳🇱', name: 'Nederlands' },
    pl:        { flag: '🇵🇱', name: 'Polski' },
    cs:        { flag: '🇨🇿', name: 'Čeština' },
    et:        { flag: '🇪🇪', name: 'Eesti' },
    lv:        { flag: '🇱🇻', name: 'Latviešu' },
    lt:        { flag: '🇱🇹', name: 'Lietuvių' },
    ja:        { flag: '🇯🇵', name: '日本語' },
    ko:        { flag: '🇰🇷', name: '한국어' },
    'zh-hans': { flag: '🇨🇳', name: '简体中文' },
    'zh-hant': { flag: '🇹🇼', name: '繁體中文' },
    zh:        { flag: '🇨🇳', name: '中文' },
  };

  // flagFor returns the flag emoji for an adventure's BCP 47 tag,
  // or '' when we don't recognise it. Tries the full lowercased tag
  // first ("pt-BR" → "pt-br" → 🇧🇷), then falls back to the primary
  // subtag ("en-US" → "en" → 🇬🇧). Authors writing in tlh / la / oc
  // get an empty flag and only the bare code.
  function flagFor(code) {
    if (!code) return '';
    const c = String(code).trim().toLowerCase();
    if (LANG_LABEL[c] && LANG_LABEL[c].flag) return LANG_LABEL[c].flag;
    const primary = c.split('-')[0];
    if (LANG_LABEL[primary] && LANG_LABEL[primary].flag) return LANG_LABEL[primary].flag;
    return '';
  }

  function available() {
    return Object.keys(DICT);
  }

  function pickInitial() {
    const saved = localStorage.getItem('zv-lang');
    if (saved && DICT[saved]) return saved;
    const browser = (navigator.language || '').slice(0, 2).toLowerCase();
    return DICT[browser] ? browser : 'en';
  }

  let currentLang = pickInitial();
  document.documentElement.setAttribute('lang', currentLang);

  function t(key, vars) {
    const table = DICT[currentLang] || {};
    let s = table[key];
    if (s === undefined) s = key; // fall through to the English source text
    if (vars) {
      for (const k of Object.keys(vars)) {
        s = s.replace(new RegExp('\\{\\{\\s*' + k + '\\s*\\}\\}', 'g'), vars[k]);
      }
    }
    return s;
  }

  function setLang(code) {
    if (!DICT[code] || code === currentLang) return;
    currentLang = code;
    localStorage.setItem('zv-lang', code);
    // Full reload is the simplest way to re-run every t() call across
    // the app. The login/localStorage session survives the reload.
    location.reload();
  }

  function getLang() { return currentLang; }

  // makeLangPicker returns a <select> pre-populated with the available
  // languages. Each option renders as "🇬🇧 English" (flag + native
  // name) so users can spot their language without reading. Styling
  // is up to the caller — the sidebar footer adds the "ghost"-style
  // classes to match its siblings.
  function makeLangPicker(opts = {}) {
    const sel = document.createElement('select');
    sel.className = opts.className || 'zv-lang-picker';
    if (opts.title) sel.title = opts.title;
    for (const code of available()) {
      const label = LANG_LABEL[code];
      const o = document.createElement('option');
      o.value = code;
      o.textContent = label ? label.flag + ' ' + label.name : code;
      if (code === currentLang) o.selected = true;
      sel.append(o);
    }
    sel.addEventListener('change', () => setLang(sel.value));
    return sel;
  }

  window.zvI18n = { t, setLang, getLang, available, makeLangPicker, flagFor };
  // Shorthand so call sites read cleanly: t('Save')
  window.t = t;
})();
