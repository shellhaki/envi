// The install-script Worker for install.envisecrets.com, so installing the CLI is a one-liner.
// The scripts are bundled in at deploy time (see the [[rules]] in install.wrangler.toml).

import installSh from "../../scripts/install.sh";
import installPs1 from "../../scripts/install.ps1";

const sh = { "Content-Type": "text/x-sh; charset=utf-8" };
const ps1 = { "Content-Type": "text/plain; charset=utf-8" };

export function serveInstall(request: Request): Response {
  const { pathname } = new URL(request.url);
  if (request.method !== "GET" && request.method !== "HEAD") return new Response("method not allowed", { status: 405 });
  if (pathname === "/health") return Response.json({ status: "ok" });
  if (pathname === "/install.ps1") return new Response(installPs1, { headers: ps1 });
  // curl | sh ignores the path, so every other path serves the shell script.
  return new Response(installSh, { headers: sh });
}

export default { fetch: serveInstall };
