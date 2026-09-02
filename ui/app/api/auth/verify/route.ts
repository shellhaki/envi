import { cookies } from "next/headers";
import { upstream } from "@/lib/server-api";
import { createSession, sessionCookie, sessionOptions } from "@/lib/web-session";
export async function POST(request: Request) {
  const response = await upstream("/auth/verify-otp", { method: "POST", body: await request.text() });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) return Response.json(body, { status: response.status });
  const store = await cookies();
  store.set(sessionCookie, createSession(body.access_token, body.refresh_token, body.expires_in), sessionOptions);
  return Response.json({ ok: true });
}
