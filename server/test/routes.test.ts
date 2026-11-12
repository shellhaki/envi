import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { app } from "../src/app";
import { canAnnounce, canManage, operator, signedIn } from "../src/middleware";
import { instanceAdmin } from "../src/routes/instance";

// docs/ENDPOINTS.md is the API contract. Each row: | METHOD | `path` | middleware | input | description |
function documented() {
  const text = readFileSync(new URL("../../docs/ENDPOINTS.md", import.meta.url), "utf8");
  return text
    .split("\n")
    .filter((line) => /^\| (GET|POST|PUT|PATCH|DELETE) \|/.test(line))
    .map((line) => {
      const [, method, path, middleware] = line.split("|").map((cell) => cell.trim());
      return { route: `${method} ${path.replaceAll("`", "")}`, middleware };
    });
}

function served() {
  const handlers = new Map<string, unknown[]>();
  for (const r of app.routes) {
    const key = `${r.method} ${r.path}`;
    handlers.set(key, [...(handlers.get(key) ?? []), r.handler]);
  }
  return handlers;
}

test("every route is documented in ENDPOINTS.md, and nothing more", () => {
  expect([...served().keys()].sort()).toEqual(documented().map((d) => d.route).sort());
});

test("the documented middleware is the middleware that runs", () => {
  const needs: Record<string, unknown[]> = {
    none: [],
    Signature: [],
    Auth: [signedIn],
    Owner: [signedIn],
    Operator: [signedIn, operator],
    Manage: [signedIn, operator, canManage],
    Announce: [signedIn, operator, canAnnounce],
    "Instance admin": [signedIn, instanceAdmin],
  };
  const routes = served();
  for (const { route, middleware } of documented()) {
    const chain = routes.get(route)!;
    const guards = chain.filter((h) => [signedIn, operator, canManage, canAnnounce, instanceAdmin].includes(h as never));
    expect({ route, guards }).toEqual({ route, guards: needs[middleware] });
  }
});
