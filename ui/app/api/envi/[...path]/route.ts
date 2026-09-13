import { cookies, headers } from "next/headers";
import { upstream } from "@/lib/server-api";
import { createSession, readSession, sessionCookie, sessionOptions } from "@/lib/web-session";

type Rotated = { access: string; refresh: string; expiresIn?: number };

// Refresh tokens are single-use, so N concurrent requests redeeming the same
// one means one winner and N-1 spurious 401s. Keyed by the token being spent,
// this collapses them into a single redemption whose result they all share.
//
// It only covers requests handled by the same server instance — the browser
// side retries through POST /api/auth/refresh for the rest.
const inflight = new Map<string, Promise<Rotated | null>>();

function rotateOnce(refresh: string): Promise<Rotated | null> {
  const existing = inflight.get(refresh);
  if (existing) return existing;
  const pending = (async (): Promise<Rotated | null> => {
    const rotated = await upstream("/auth/refresh", { method: "POST", body: JSON.stringify({ refresh_token: refresh }) });
    if (!rotated.ok) return null;
    const tokens = await rotated.json().catch(() => ({}));
    if (!tokens.access_token || !tokens.refresh_token) return null;
    return { access: tokens.access_token, refresh: tokens.refresh_token, expiresIn: tokens.expires_in };
  })();
  inflight.set(refresh, pending);
  // Held briefly after settling so a request that arrives a moment late joins
  // the result instead of redeeming a token that is already spent. The key is
  // the old token, which nothing will present again once the cookie updates.
  void pending.finally(() => setTimeout(() => inflight.delete(refresh), 10_000));
  return pending;
}
async function handle(request: Request, { params }: { params: Promise<{ path: string[] }> }) {
  if (!["GET", "HEAD", "OPTIONS"].includes(request.method)) {
    const origin = request.headers.get("origin");
    if (origin && new URL(origin).host !== (await headers()).get("host")) return Response.json({ error: "invalid request origin" }, { status: 403 });
  }
  const store = await cookies();
  const session = readSession(store.get(sessionCookie)?.value);
  const path = "/" + (await params).path.join("/");
  const body = ["GET", "HEAD"].includes(request.method) ? undefined : await request.arrayBuffer();
  const send = (token?: string) => upstream(path, { method: request.method, body, headers: token ? { Authorization: `Bearer ${token}` } : {} });
  let response = await send(session?.access);
  if (response.status === 401) {
    if (!session) return response;
    const rotated = await rotateOnce(session.refresh);
    // A failed rotation must not clear the cookie: another instance may have
    // rotated it already, and dropping it would sign the visitor out
    // mid-navigation. The browser retries this request through the refresh
    // endpoint instead.
    if (!rotated) return response;
    store.set(sessionCookie, createSession(rotated.access, rotated.refresh, rotated.expiresIn), sessionOptions);
    response = await send(rotated.access);
  }
  return new Response(response.body, { status: response.status, headers: { "Content-Type": response.headers.get("content-type") || "application/json" } });
}
export const GET = handle; export const POST = handle; export const PUT = handle; export const PATCH = handle; export const DELETE = handle;
