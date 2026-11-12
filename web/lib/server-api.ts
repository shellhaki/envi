import "server-only";
import { cookies } from "next/headers";
import { readSession, sessionCookie } from "@/lib/web-session";

// `??` would accept an empty ENVI_API_URL and leave base "", making every
// upstream call a relative URL that fetch rejects. The Go side already treats
// empty as unset; match it.
const base = process.env.ENVI_API_URL || "http://127.0.0.1:8080";
export async function upstream(path: string, init: RequestInit = {}) { return fetch(base + path, { ...init, cache: "no-store", headers: { "Content-Type": "application/json", ...init.headers } }); }
export async function account() {
  const session = readSession((await cookies()).get(sessionCookie)?.value);
  if (!session) return null;
  const response = await upstream("/me", { headers: { Authorization: `Bearer ${session.access}` } });
  return response.ok ? response.json() as Promise<{ ID: string; Email: string; OrganizationID: string; Role: "" | "admin" | "contributor" }> : null;
}

export type Instance = { mode: string; sign_in: "email" | "password"; billing: boolean };

// instance says how the API signs people in. A self-hosted instance signs in with a password and has no billing.
export async function instance(): Promise<Instance> {
  const response = await upstream("/instance").catch(() => null);
  if (!response?.ok) return { mode: "hosted", sign_in: "email", billing: true };
  return response.json();
}
export function firstName(email: string) { return email.split("@", 1)[0]; }
