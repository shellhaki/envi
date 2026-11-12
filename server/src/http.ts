import type { Context } from "hono";
import { routePath } from "hono/route";
import type { AppEnv } from "./deps";

export type Ctx = Context<AppEnv>;

type FieldType = "string" | "number" | "boolean" | "number?" | "strings";
type Schema = Record<string, FieldType>;
type Value<T extends FieldType> = T extends "string" ? string
  : T extends "number" ? number
  : T extends "boolean" ? boolean
  : T extends "number?" ? number | null
  : Record<string, string> | null;

// readBody parses a JSON object, using defaults for missing fields ("", 0, false, or null for maps and "number?").
// It returns null when the body isn't a JSON object or a field has the wrong type.
export async function readBody<S extends Schema>(c: Ctx, schema: S): Promise<{ [K in keyof S]: Value<S[K]> } | null> {
  let body: unknown;
  try {
    body = await c.req.json();
  } catch {
    return null;
  }
  if (!isObject(body)) return null;

  const out: Record<string, unknown> = {};
  for (const [name, type] of Object.entries(schema)) {
    const value = body[name];
    if (value === undefined || value === null) {
      out[name] = { string: "", number: 0, boolean: false, "number?": null, strings: null }[type];
      continue;
    }
    const ok =
      (type === "string" && typeof value === "string") ||
      ((type === "number" || type === "number?") && Number.isInteger(value)) ||
      (type === "boolean" && typeof value === "boolean") ||
      (type === "strings" && isObject(value) && Object.values(value).every((v) => typeof v === "string"));
    if (!ok) return null;
    out[name] = value;
  }
  return out as { [K in keyof S]: Value<S[K]> };
}

// readStringMap parses a body that is itself a {KEY: "value"} object.
export async function readStringMap(c: Ctx): Promise<Record<string, string> | null> {
  try {
    const body = await c.req.json();
    if (isObject(body) && Object.values(body).every((v) => typeof v === "string")) return body as Record<string, string>;
  } catch {}
  return null;
}

export function isEmail(value: string) {
  return /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)*$/.test(value);
}

export function fail(c: Ctx, status: number, code: string, error: string) {
  return c.json({ code, error }, status as 400);
}

export function forbidden(c: Ctx) {
  return fail(c, 403, "forbidden", "access denied");
}

// route returns the matched route pattern, e.g. "/me/api-keys/:id".
export function route(c: Ctx) {
  return routePath(c, -1);
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// param reads a path parameter like :id.
export function param(c: Ctx, name: string): string {
  return c.req.param(name) ?? "";
}
