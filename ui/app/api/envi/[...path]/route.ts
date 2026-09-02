import { cookies, headers } from "next/headers";
import { upstream } from "@/lib/server-api";
import { createSession, readSession, sessionCookie, sessionOptions } from "@/lib/web-session";
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
    const rotated = await upstream("/auth/refresh", { method: "POST", body: JSON.stringify({ refresh_token: session.refresh }) });
    // Refresh tokens are single-use, so parallel requests race here and all but
    // one lose. Losing must not clear the cookie: the winner has already stored a
    // valid session, and dropping it would sign the visitor out mid-navigation.
    if (!rotated.ok) return response;
    const tokens = await rotated.json().catch(() => ({}));
    if (!tokens.access_token || !tokens.refresh_token) return response;
    store.set(sessionCookie, createSession(tokens.access_token, tokens.refresh_token, tokens.expires_in), sessionOptions);
    response = await send(tokens.access_token);
  }
  return new Response(response.body, { status: response.status, headers: { "Content-Type": response.headers.get("content-type") || "application/json" } });
}
export const GET = handle; export const POST = handle; export const PUT = handle; export const PATCH = handle; export const DELETE = handle;
