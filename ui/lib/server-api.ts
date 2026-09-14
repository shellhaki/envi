import "server-only";
import { cookies } from "next/headers";
import { readSession, sessionCookie } from "@/lib/web-session";

// `??` would accept an empty ENVI_API_URL and leave base "", making every
// upstream call a relative URL that fetch rejects. The Go side already treats
// empty as unset; match it.
const base = process.env.ENVI_API_URL || "http://localhost:8787";
export async function upstream(path: string, init: RequestInit = {}) { return fetch(base + path, { ...init, cache: "no-store", headers: { "Content-Type": "application/json", ...init.headers } }); }
export async function account() {
  const session = readSession((await cookies()).get(sessionCookie)?.value);
  if (!session) return null;
  const response = await upstream("/me", { headers: { Authorization: `Bearer ${session.access}` } });
  return response.ok ? response.json() as Promise<{ ID: string; Email: string; OrganizationID: string }> : null;
}
export function firstName(email: string) { return email.split("@", 1)[0]; }
export async function publicConfig(): Promise<{ beta: boolean }> {
  try {
    const response = await upstream("/config");
    if (!response.ok) return { beta: false };
    return await response.json();
  } catch {
    return { beta: false };
  }
}
