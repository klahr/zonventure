// The name this instance presents itself under (ZV_SITE_NAME). The
// server writes it into each app's index.html as the
// application-name meta; "Zonventure" when unset or outside a page.
export function siteName(): string {
  const v = typeof document === "undefined"
    ? undefined
    : document.querySelector<HTMLMetaElement>('meta[name="application-name"]')?.content.trim();
  return v || "Zonventure";
}

// The header wordmark: Zonventure's lowercase "zonventure" by default,
// the site name as written otherwise.
export function siteWordmark(): string {
  const n = siteName();
  return n === "Zonventure" ? "zonventure" : n;
}
