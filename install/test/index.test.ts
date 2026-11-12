import { expect, test } from "bun:test";
import { serveInstall } from "../index";

const script = (name: string) => Bun.file(new URL(`../../scripts/${name}`, import.meta.url)).text();
const get = (path: string, method = "GET") => serveInstall(new Request(`https://install.envisecrets.com${path}`, { method }));

test("every path but /install.ps1 and /health serves install.sh", async () => {
  for (const path of ["/", "/install.sh", "/anything"]) {
    const reply = get(path);
    expect(reply.headers.get("Content-Type")).toBe("text/x-sh; charset=utf-8");
    expect(await reply.text()).toBe(await script("install.sh"));
  }
});

test("/install.ps1 serves the PowerShell script", async () => {
  const reply = get("/install.ps1");
  expect(reply.headers.get("Content-Type")).toBe("text/plain; charset=utf-8");
  expect(await reply.text()).toBe(await script("install.ps1"));
});

test("/health answers ok and writes are refused", async () => {
  expect(await get("/health").json()).toEqual({ status: "ok" });
  expect(get("/", "POST").status).toBe(405);
});
