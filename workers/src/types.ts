export type Env = {
  DB: D1Database;
  CACHE: KVNamespace;
  ENVI_ENCRYPTION_KEY: string;
  ENVI_WEB_URL: string;
  ENVIRONMENT: string;
  RESEND_API_KEY: string;
  RESEND_FROM: string;
};

export type Actor =
  | { kind: "user"; userId: string }
  | { kind: "service"; serviceId: string; environmentId: string; permission: Permission };

export type Permission = "read" | "write" | "manage";

export type Vars = { actor: Actor };

export const ACCESS_TTL_SECONDS = 15 * 60;
export const REFRESH_TTL_SECONDS = 30 * 24 * 60 * 60;
