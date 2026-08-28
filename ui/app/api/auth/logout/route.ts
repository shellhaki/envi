import { cookies } from "next/headers";
import { accessCookie, refreshCookie, upstream } from "@/lib/server-api";
export async function POST() {
  const store = await cookies();
  const refresh = store.get(refreshCookie)?.value;
  if (refresh) await upstream("/auth/logout", { method: "POST", body: JSON.stringify({ refresh_token: refresh }) });
  store.delete(accessCookie); store.delete(refreshCookie);
  return new Response(null, { status: 204 });
}
