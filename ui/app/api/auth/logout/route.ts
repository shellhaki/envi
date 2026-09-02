import { cookies } from "next/headers";
import { upstream } from "@/lib/server-api";
import { readSession, sessionCookie } from "@/lib/web-session";
export async function POST() {
  const store = await cookies();
  const session = readSession(store.get(sessionCookie)?.value);
  if (session) await upstream("/auth/logout", { method: "POST", body: JSON.stringify({ refresh_token: session.refresh }) });
  store.delete(sessionCookie);
  return new Response(null, { status: 204 });
}
