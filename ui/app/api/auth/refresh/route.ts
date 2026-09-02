import { NextResponse } from "next/server";
import { cookies } from "next/headers";
import { upstream } from "@/lib/server-api";
import { accessExpired, createSession, readSession, sessionCookie, sessionOptions } from "@/lib/web-session";

// Only same-origin absolute paths, so ?next= cannot be used as an open redirect.
function destination(value: string | null) {
  return value && value.startsWith("/") && !value.startsWith("//") ? value : "/dashboard";
}

// The single place the session is refreshed. Proxy sends document requests here
// when the access token is due, so a rotation happens once per expiry rather
// than once per in-flight request — refresh tokens are single-use, and parallel
// redemptions would revoke the session.
export async function GET(request: Request) {
  const url = new URL(request.url);
  const next = destination(url.searchParams.get("next"));
  const store = await cookies();
  const session = readSession(store.get(sessionCookie)?.value);
  if (!session) return NextResponse.redirect(new URL("/auth", url), { status: 303 });
  // Another request may have rotated the cookie while this one was queued.
  if (!accessExpired(session)) return NextResponse.redirect(new URL(next, url), { status: 303 });

  const rotated = await upstream("/auth/refresh", { method: "POST", body: JSON.stringify({ refresh_token: session.refresh }) });
  if (!rotated.ok) {
    const dead = NextResponse.redirect(new URL("/auth", url), { status: 303 });
    dead.cookies.delete(sessionCookie);
    return dead;
  }
  const tokens = await rotated.json().catch(() => ({}));
  if (!tokens.access_token || !tokens.refresh_token) return NextResponse.redirect(new URL("/auth", url), { status: 303 });
  const response = NextResponse.redirect(new URL(next, url), { status: 303 });
  response.cookies.set(sessionCookie, createSession(tokens.access_token, tokens.refresh_token, tokens.expires_in), sessionOptions);
  return response;
}
