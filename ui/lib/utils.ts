export function filterKeys(values: Record<string, string>, query: string) {
  const needle = query.toLowerCase();
  return Object.keys(values).filter((key) => key.toLowerCase().includes(needle)).sort();
}

export function initials(email = "") {
  return email.slice(0, 2).toUpperCase() || "EN";
}

// parseDotenv turns a .env file body into key/value pairs. It tolerates blank
// lines, `#` comments, an optional `export ` prefix, `=` inside values, and
// single/double-quoted values (with basic escapes in double quotes). Keys that
// are not valid env identifiers are skipped so a stray line can't poison the set.
export function parseDotenv(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    const body = line.startsWith("export ") ? line.slice(7).trimStart() : line;
    const eq = body.indexOf("=");
    if (eq < 1) continue;
    const key = body.slice(0, eq).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_.]*$/.test(key)) continue;
    let value = body.slice(eq + 1).trim();
    const quote = value[0];
    if ((quote === '"' || quote === "'") && value.length >= 2 && value.endsWith(quote)) {
      value = value.slice(1, -1);
      if (quote === '"') value = value.replace(/\\n/g, "\n").replace(/\\"/g, '"').replace(/\\\\/g, "\\");
    } else {
      const comment = value.indexOf(" #");
      if (comment >= 0) value = value.slice(0, comment).trimEnd();
    }
    out[key] = value;
  }
  return out;
}

// formatDotenv is the inverse of parseDotenv: key/value pairs back into a .env
// file body, keys sorted so the same set always renders the same text. It
// matches what `envi pull` writes, so a value copied from the dashboard and one
// pulled by the CLI are byte-identical.
export function formatDotenv(values: Record<string, string>): string {
  return Object.keys(values).sort()
    .map((key) => `${key}=${quoteValue(values[key])}`)
    .join("\n");
}

// quoteValue decides whether a value can be written bare. Newlines are the case
// that matters: a private key written raw spreads across lines and the file no
// longer parses. Leading and trailing spaces are quoted for the same reason —
// parsing trims them, so writing them bare loses them.
function quoteValue(value: string): string {
  if (!value || (!/["\\\n\r]/.test(value) && value.trim() === value)) return value;
  return JSON.stringify(value);
}

// relativeTime renders an ISO timestamp as a short "3m ago" style label.
export function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (!then) return "";
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 45) return "just now";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.round(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  const days = Math.round(hrs / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.round(days / 30);
  if (months < 12) return `${months}mo ago`;
  return `${Math.round(months / 12)}y ago`;
}
