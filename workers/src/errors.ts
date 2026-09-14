import type { Context } from "hono";

/** Matches the Go server's body exactly: the CLI parses both fields. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

export const badRequest = (msg: string) => new ApiError(400, "invalid_request", msg);
export const unauthorized = (msg = "authentication required") => new ApiError(401, "unauthenticated", msg);
export const forbidden = (msg = "access denied") => new ApiError(403, "forbidden", msg);
export const notFound = (msg: string) => new ApiError(404, "not_found", msg);
export const conflict = (code: string, msg: string) => new ApiError(409, code, msg);
export const tooMany = (msg: string) => new ApiError(429, "rate_limited", msg);
export const internal = (msg: string) => new ApiError(500, "internal", msg);

export function errorBody(err: unknown): { status: number; body: { code: string; error: string } } {
  if (err instanceof ApiError) {
    return { status: err.status, body: { code: err.code, error: err.message } };
  }
  return { status: 500, body: { code: "internal", error: "internal error" } };
}

export function fail(c: Context, err: unknown) {
  // Anything that is not a deliberate ApiError is a bug: log the cause, return
  // the generic message. Without this the client sees "internal error" and the
  // logs say nothing at all.
  if (!(err instanceof ApiError)) console.error("unhandled error:", err);
  const { status, body } = errorBody(err);
  return c.json(body, status as 400);
}
