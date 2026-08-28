import { cookies } from "next/headers";
import { accessCookie, cookieOptions, refreshCookie, upstream } from "@/lib/server-api";
export async function POST(request: Request) {
  const response = await upstream("/auth/verify-otp", { method: "POST", body: await request.text() });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) return Response.json(body, { status: response.status });
  const store = await cookies();
  store.set(accessCookie, body.access_token, { ...cookieOptions, maxAge: 900 });
  store.set(refreshCookie, body.refresh_token, { ...cookieOptions, maxAge: 2592000 });
  return Response.json({ ok: true });
}
