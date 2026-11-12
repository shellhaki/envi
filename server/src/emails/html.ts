export function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&#34;")
    .replaceAll("'", "&#39;");
}

// hostOf strips the scheme, path and port from a URL.
export function hostOf(url: string): string {
  const host = url.replace(/^https?:\/\//, "");
  const end = host.search(/[/:]/);
  return end > 0 ? host.slice(0, end) : host;
}
