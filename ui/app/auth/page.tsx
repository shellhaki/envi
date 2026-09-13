import Link from "next/link";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import { account, firstName, publicConfig } from "@/lib/server-api";
import AuthForm from "./auth-form";
export default async function AuthPage({ searchParams }: { searchParams: Promise<{ next?: string | string[] }> }) {
  const rawNext = (await searchParams).next;
  const next = (Array.isArray(rawNext) ? rawNext[0] : rawNext) ?? "";
  // Only allow same-site relative paths; reject "//host" protocol-relative URLs.
  const safeNext = next.startsWith("/") && !next.startsWith("//") ? next : undefined;
  const [user, config] = await Promise.all([account(), publicConfig()]);
  return <main className="auth-page"><header><Brand /><ThemeToggle /></header><section className="auth-box">{user ? <><span className="kicker">Signed in</span><h1>Welcome back, {firstName(user.Email)}</h1><p>Your Envi session is active.</p><Link className="button accent large" href={safeNext ?? "/dashboard"}>{safeNext ? "Continue" : "Go to dashboard"}</Link><Link className="text-link" href="/">Return home</Link></> : <><span className="kicker">{config.beta ? "Private beta" : "Passwordless access"}</span><h1>Sign in to Envi</h1><p>{config.beta ? "Access is limited to invited testers right now. We'll send a short-lived code to your email." : "We will send a short-lived code to your email."}</p><AuthForm next={safeNext} /><small>By continuing, you agree to use Envi responsibly and keep account access secure.</small></>}</section></main>;
}
