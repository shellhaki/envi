import { NextResponse, type NextRequest } from "next/server";
import { accessExpired, readSession, sessionCookie } from "@/lib/web-session";
import { CLOUD_URL, DOCS_URL } from "@/lib/site";

const SITE_HOSTS = ["envisecrets.com", "www.envisecrets.com"];
const CLOUD_PATHS = ["/dashboard", "/auth", "/invite", "/device", "/billing", "/settings", "/admin", "/api"];

// Server Components cannot write cookies, so a page render can never rotate an
// expired session itself — it would just bounce the visitor to /auth while a
// valid refresh token sat unused in the cookie. Proxy catches document requests
// beforehand and sends them through the refresh Route Handler, which can write.
//
// This stays deliberately network-free: Proxy runs on every route, including
// prefetches, and refresh tokens are single-use. The access expiry is read
// straight from the signed cookie, so only a genuinely due session redirects.
export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;
  const host = request.headers.get("host") ?? "";

  // The docs are app/docs, served at the root of their own host: docs.envisecrets.com/cli
  // is app/docs/cli. Locally that host is docs.localhost:3000.
  if (host.startsWith("docs.")) {
    const url = request.nextUrl.clone();
    url.pathname = pathname === "/" ? "/docs" : `/docs${pathname}`;
    return NextResponse.rewrite(url);
  }
  if (pathname === "/docs" || pathname.startsWith("/docs/")) {
    // On envisecrets.com and Envi Cloud the docs are docs.envisecrets.com; elsewhere, docs. in front of the host.
    const url = new URL(host.split(":")[0].endsWith("envisecrets.com") ? DOCS_URL : `${request.nextUrl.protocol}//docs.${host}`);
    url.pathname = pathname.slice("/docs".length) || "/";
    url.search = search;
    return NextResponse.redirect(url);
  }

  // envisecrets.com is the self-hosting site; the hosted dashboard is Envi Cloud. Old links to the
  // dashboard on the main site, such as invitations in past emails, move there with their path.
  const bareHost = host.split(":")[0];
  if (SITE_HOSTS.includes(bareHost) && CLOUD_PATHS.some((p) => pathname === p || pathname.startsWith(`${p}/`))) {
    return NextResponse.redirect(new URL(pathname + search, CLOUD_URL), 308);
  }
  // The main site has no sign-in, so an old session cookie there is never refreshed.
  if (SITE_HOSTS.includes(bareHost)) return NextResponse.next();
  if (bareHost === new URL(CLOUD_URL).hostname && pathname === "/") {
    return NextResponse.redirect(new URL("/dashboard", CLOUD_URL));
  }

  // Route Handlers answer fetch() calls, which must not be answered with a
  // redirect; /api/envi refreshes inline instead.
  if (pathname.startsWith("/api/")) return NextResponse.next();

  const session = readSession(request.cookies.get(sessionCookie)?.value);
  if (!session || !accessExpired(session)) return NextResponse.next();

  const url = request.nextUrl.clone();
  url.pathname = "/api/auth/refresh";
  url.search = "";
  url.searchParams.set("next", pathname + search);
  return NextResponse.redirect(url);
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico|.*\\.(?:png|jpg|jpeg|gif|svg|ico|webp|woff2?)$).*)"],
};
