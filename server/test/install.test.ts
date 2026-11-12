import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { serveInstall } from "../src/install";

const script = (name: string) => readFileSync(new URL(`../../scripts/${name}`, import.meta.url), "utf8");
const get = (path: string, method = "GET") => serveInstall(new Request(`https://install.envisecrets.com${path}`, { method }));

test("every path but /install.ps1 and /health serves install.sh", async () => {
  for (const path of ["/", "/install.sh", "/anything"]) {
    const reply = await get(path);
    expect(reply.headers.get("Content-Type")).toBe("text/x-sh; charset=utf-8");
    expect(await reply.text()).toBe(script("install.sh"));
  }
});

test("/install.ps1 serves the PowerShell script", async () => {
  const reply = await get("/install.ps1");
  expect(reply.headers.get("Content-Type")).toBe("text/plain; charset=utf-8");
  expect(await reply.text()).toBe(script("install.ps1"));
});

test("/health answers ok and writes are refused", async () => {
  expect(await (await get("/health")).json()).toEqual({ status: "ok" });
  expect((await get("/", "POST")).status).toBe(405);
});
