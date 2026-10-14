// The name this instance presents itself under (ZV_SITE_NAME). The
// server writes it into each app's index.html as the
// application-name meta; "Zonventure" when unset or outside a page.
export function siteName(): string {
  const v = typeof document === "undefined"
    ? undefined
    : document.querySelector<HTMLMetaElement>('meta[name="application-name"]')?.content.trim();
  return v || "Zonventure";
}

// The currency this instance sells in (ZV_CURRENCY), ISO 4217 upper
// case; the server writes it into each app's index.html like the name.
export function siteCurrency(): string {
  const v = typeof document === "undefined"
    ? undefined
    : document.querySelector<HTMLMetaElement>('meta[name="zv-currency"]')?.content.trim();
  return (v || "sek").toUpperCase();
}

// The header wordmark: Zonventure's lowercase "zonventure" by default,
// the site name as written otherwise.
export function siteWordmark(): string {
  const n = siteName();
  return n === "Zonventure" ? "zonventure" : n;
}
