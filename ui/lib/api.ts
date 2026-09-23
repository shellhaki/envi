"use client";

// Talking to the Envi API from the browser. Requests go to this app's own
// /api/envi proxy, which holds the session cookie and adds the bearer token;
// the browser never sees a token.

// One rotation per expiry, no matter how many requests notice at once. A page
// fires several fetches in parallel, so without this they each redeem the same
// single-use refresh token and all but one come back "authentication required"
// — a session that looks broken until you reload.
let rotating: Promise<boolean> | null = null;

export function ensureSession() {
  rotating ??= fetch("/api/auth/refresh", { method: "POST" })
    .then((r) => r.ok)
    .catch(() => false)
    .finally(() => { rotating = null; });
  return rotating;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const send = () => fetch("/api/envi" + path, { ...init, headers: { "Content-Type": "application/json", ...init.headers } });
  let r = await send();
  if (r.status === 401 && await ensureSession()) r = await send();
  const b = await r.json().catch(() => ({}));
  // Status travels with the error so callers can branch on it rather than
  // matching on message text.
  if (!r.ok) throw Object.assign(new Error(b.error || "Request failed"), { status: r.status });
  return b as T;
}
