import "server-only";
import { cookies } from "next/headers";

const base = process.env.ENVI_API_URL ?? "http://127.0.0.1:8080";
export const accessCookie = "envi_access";
export const refreshCookie = "envi_refresh";
export const cookieOptions = { httpOnly: true, sameSite: "strict" as const, secure: process.env.NODE_ENV === "production", path: "/", priority: "high" as const };
export async function upstream(path: string, init: RequestInit = {}) { return fetch(base + path, { ...init, cache: "no-store", headers: { "Content-Type": "application/json", ...init.headers } }); }
export async function account() {
  const token = (await cookies()).get(accessCookie)?.value;
  if (!token) return null;
  const response = await upstream("/me", { headers: { Authorization: `Bearer ${token}` } });
  return response.ok ? response.json() as Promise<{ ID: string; Email: string; OrganizationID: string }> : null;
}
export function firstName(email: string) { return email.split("@", 1)[0]; }
