import { createHmac, timingSafeEqual } from "node:crypto";

export const sessionCookie = "envi_session";
export const sessionOptions = { httpOnly: true, sameSite: "strict" as const, secure: process.env.NODE_ENV === "production", path: "/", priority: "high" as const, maxAge: 2592000 };
// Refresh slightly ahead of expiry so a request in flight cannot land on a token
// that expired in transit.
export const accessSkewSeconds = 30;
const sessionTTL = 2592000;
const fallbackAccessTTL = 900;
// aexp is when the upstream access token expires; exp is when the session cookie
// itself does. Keeping them apart lets Proxy decide whether a refresh is due by
// reading the cookie alone, with no call upstream.
type Session = { access: string; refresh: string; aexp: number; exp: number };

function secret() {
  const value = process.env.ENVI_SESSION_SECRET ?? (process.env.NODE_ENV === "production" ? "" : "envi-local-development-session-secret");
  if (value.length < 32) throw new Error("ENVI_SESSION_SECRET must be at least 32 characters");
  return value;
}
function encode(value: object) { return Buffer.from(JSON.stringify(value)).toString("base64url"); }
function sign(value: string) { return createHmac("sha256", secret()).update(value).digest("base64url"); }
export function createSession(access: string, refresh: string, expiresIn?: number) {
  const now = Math.floor(Date.now() / 1000);
  // A missing or nonsensical expires_in must still produce a future aexp, or
  // Proxy would redirect to refresh forever.
  const ttl = typeof expiresIn === "number" && Number.isFinite(expiresIn) && expiresIn > 0 ? Math.floor(expiresIn) : fallbackAccessTTL;
  const body = `${encode({ alg: "HS256", typ: "JWT" })}.${encode({ access, refresh, aexp: now + ttl, exp: now + sessionTTL })}`;
  return `${body}.${sign(body)}`;
}
export function readSession(value?: string): Session | null {
  if (!value) return null;
  const [header, payload, signature, extra] = value.split(".");
  if (!header || !payload || !signature || extra) return null;
  const expected = Buffer.from(sign(`${header}.${payload}`));
  const received = Buffer.from(signature);
  if (expected.length !== received.length || !timingSafeEqual(expected, received)) return null;
  try {
    const session = JSON.parse(Buffer.from(payload, "base64url").toString()) as Session;
    if (!session.access || !session.refresh || !(session.exp > Date.now() / 1000)) return null;
    // Cookies written before aexp existed are treated as due for refresh rather
    // than rejected, so upgrades do not sign everyone out.
    return { ...session, aexp: typeof session.aexp === "number" ? session.aexp : 0 };
  } catch { return null; }
}
export function accessExpired(session: Session, skew = accessSkewSeconds) {
  // A missing or non-numeric aexp must read as expired: erring towards one extra
  // refresh is recoverable, erring towards "still valid" strands the session.
  if (typeof session.aexp !== "number" || !Number.isFinite(session.aexp)) return true;
  return session.aexp - skew <= Date.now() / 1000;
}
