import { cookies, headers } from "next/headers";
import { accessCookie, cookieOptions, refreshCookie, upstream } from "@/lib/server-api";
async function handle(request: Request, { params }: { params: Promise<{ path: string[] }> }) {
  if (!["GET", "HEAD", "OPTIONS"].includes(request.method)) {
    const origin = request.headers.get("origin");
    if (origin && new URL(origin).host !== (await headers()).get("host")) return Response.json({ error: "invalid request origin" }, { status: 403 });
  }
  const store = await cookies();
  const path = "/" + (await params).path.join("/");
  const body = ["GET", "HEAD"].includes(request.method) ? undefined : await request.arrayBuffer();
  const send = (token?: string) => upstream(path, { method: request.method, body, headers: token ? { Authorization: `Bearer ${token}` } : {} });
  let response = await send(store.get(accessCookie)?.value);
  if (response.status === 401) {
    const refresh = store.get(refreshCookie)?.value;
    if (!refresh) return response;
    const rotated = await upstream("/auth/refresh", { method: "POST", body: JSON.stringify({ refresh_token: refresh }) });
    if (!rotated.ok) { store.delete(accessCookie); store.delete(refreshCookie); return response; }
    const tokens = await rotated.json();
    store.set(accessCookie, tokens.access_token, { ...cookieOptions, maxAge: 900 });
    store.set(refreshCookie, tokens.refresh_token, { ...cookieOptions, maxAge: 2592000 });
    response = await send(tokens.access_token);
  }
  return new Response(response.body, { status: response.status, headers: { "Content-Type": response.headers.get("content-type") || "application/json" } });
}
export const GET = handle; export const POST = handle; export const PUT = handle; export const PATCH = handle; export const DELETE = handle;
