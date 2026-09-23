import Link from "next/link";
import { ArrowRight, ChevronDown, Clipboard, Clock, Eye, EyeOff, Pencil, Search, Shield, Trash2 } from "lucide-react";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import DemoVideo from "@/components/demo-video";
import HeroHeadline from "@/components/hero-headline";
import CookieConsent from "@/components/cookie-consent";
import { account, firstName } from "@/lib/server-api";

const DOCS = "https://docs.envisecrets.com";
const GITHUB = "https://github.com/shellhaki/envi";

// Sample rows for the product shots below. They use the dashboard's own table
// and timeline classes, so the page shows what the product actually looks like.
const SECRETS: [string, string | null][] = [
  ["DATABASE_URL", null],
  ["REDIS_URL", "redis://cache.internal:6379/0"],
  ["RESEND_API_KEY", null],
  ["SESSION_SECRET", null],
  ["STRIPE_SECRET_KEY", null],
  ["STRIPE_WEBHOOK_SECRET", null],
];

const PEOPLE: { email: string; perm: "read" | "write" | "manage"; pending?: boolean }[] = [
  { email: "amara@northwind.dev", perm: "manage" },
  { email: "diego@northwind.dev", perm: "write" },
  { email: "priya@northwind.dev", perm: "read" },
  { email: "contractor@studio.io", perm: "read", pending: true },
];
const PERM_ICON = { read: <Eye />, write: <Pencil />, manage: <Shield /> };

const EVENTS: { kind: "read" | "write" | "delete"; label: string; who: string; target: string; when: string }[] = [
  { kind: "write", label: "Updated secrets", who: "diego@northwind.dev", target: "production", when: "2m ago" },
  { kind: "read", label: "Read secrets", who: "Service token · deploy", target: "production", when: "14m ago" },
  { kind: "delete", label: "Deleted a secret", who: "amara@northwind.dev", target: "staging", when: "1h ago" },
  { kind: "read", label: "Read secrets", who: "priya@northwind.dev", target: "development", when: "3h ago" },
];
const EVENT_ICON = { read: Eye, write: Pencil, delete: Trash2 };

export default async function Home() {
  const user = await account();
  const start = user ? "/dashboard" : "/auth";

  return <div className="site-page">
    <header className="site-header">
      <div className="site-header-inner">
        <Brand />
        <nav><a href="#secrets">Product</a><a href="#access">Access</a><a href="#audit">Audit</a><a href="#pricing">Pricing</a><a href={DOCS}>Docs</a></nav>
        <div className="header-actions">
          <ThemeToggle />
          {user
            ? <><span className="welcome">Welcome back, {firstName(user.Email)}</span><Link className="button primary" href="/dashboard">Go to dashboard</Link></>
            : <><Link className="button ghost" href="/auth">Sign in</Link><Link className="button primary" href="/auth">Get started</Link></>}
        </div>
      </div>
    </header>

    <main>
      <section className="lp-hero">
        <HeroHeadline />
        <div className="lp-hero-row">
          <p>Envi keeps environment variables encrypted, versioned and scoped to the people who need them, from a laptop to production.</p>
          <div className="lp-hero-actions">
            <Link className="button accent large" href={start}>{user ? "Go to dashboard" : "Start for free"}<ArrowRight /></Link>
            <a className="button secondary large" href={DOCS}>Read the docs</a>
          </div>
        </div>
        <div className="lp-demo"><DemoVideo /></div>
      </section>

      <section className="lp-row" id="secrets">
        <div className="lp-copy">
          <span className="kicker">Secrets</span>
          <h2>Every environment, one project.</h2>
          <p>Development, staging and production live side by side, each with its own values. Values stay masked until someone reveals them, and every save bumps a revision so nobody overwrites a teammate&apos;s change without knowing.</p>
          <ul><li>Import an existing .env in one drop</li><li>Masked by default, revealed per key</li><li>A new revision on every change</li></ul>
        </div>
        <div className="lp-shot" inert>
          <div className="lp-shot-inner">
            <div className="lp-context">
              <div><span>Project</span><div className="lp-select">northwind-api<ChevronDown /></div></div>
              <div><span>Environment<em className="badge prod">Production</em></span><div className="lp-select">production<ChevronDown /></div></div>
            </div>
            <div className="panel">
              <div className="panel-head">
                <div className="lp-search"><Search />Filter keys</div>
                <div className="panel-tools"><span className="badge">rev 42</span><span className="button primary">Add secret</span></div>
              </div>
              <div className="data-table">
                <div className="thead"><span>Key</span><span>Value</span><span /></div>
                {SECRETS.map(([key, shown]) => <div className="trow" key={key}>
                  <code>{key}</code>
                  <code className="val">{shown ?? "•".repeat(12)}</code>
                  <div className="cell-actions"><span className="icon-btn">{shown ? <EyeOff /> : <Eye />}</span><span className="icon-btn"><Clipboard /></span><span className="icon-btn"><Pencil /></span></div>
                </div>)}
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="lp-row flip" id="access">
        <div className="lp-copy">
          <span className="kicker">Access</span>
          <h2>Share a project, not the whole workspace.</h2>
          <p>Invite people by email and give each one read, write or manage. Production needs its own grant, so access to a project never quietly includes the environment that matters most.</p>
          <ul><li>Read, write and manage roles</li><li>Production is granted separately</li><li>Invitations expire on their own</li></ul>
        </div>
        <div className="lp-shot" inert>
          <div className="lp-shot-inner">
            <div className="panel">
              <div className="panel-head"><h2>Collaborators</h2><span className="button primary">Invite</span></div>
              <div className="data-table">
                <div className="thead"><span>Collaborator</span><span>Access</span><span /></div>
                {PEOPLE.map((p) => <div className="trow" key={p.email}>
                  <span>{p.email}</span>
                  <span className="perm-cell">{PERM_ICON[p.perm]}{p.perm}
                    {p.pending ? <span className="badge pending"><Clock />Pending</span> : <span className="badge success">Active</span>}
                  </span>
                  <div className="cell-actions"><span className="icon-btn"><Trash2 /></span></div>
                </div>)}
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="lp-row" id="audit">
        <div className="lp-copy">
          <span className="kicker">Audit</span>
          <h2>Know who touched what, and when.</h2>
          <p>Every write and delete is recorded against the person or token that made it. Reads are logged once per environment, so a deploy that runs all day still leaves a feed you can read.</p>
          <ul><li>People and service tokens side by side</li><li>Scoped to the exact environment</li><li>Kept for every project</li></ul>
        </div>
        <div className="lp-shot" inert>
          <div className="lp-shot-inner">
            <div className="panel">
              <div className="panel-head"><h2>Activity</h2><span className="button secondary">Refresh</span></div>
              <div className="timeline">
                {EVENTS.map((e, i) => {
                  const Icon = EVENT_ICON[e.kind];
                  return <div className="activity-item" key={i}>
                    <div className={"activity-icon " + e.kind}><Icon /></div>
                    <div className="activity-body"><strong>{e.label}</strong><small>{e.who} · {e.target}</small></div>
                    <span className="activity-when">{e.when}</span>
                  </div>;
                })}
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="lp-facts" aria-label="How Envi protects your secrets">
        <div><strong>AES-256-GCM</strong><span>Every value is sealed before it reaches the database, including its history.</span></div>
        <div><strong>No passwords</strong><span>Sign in with an email code or approve a device. There is nothing to leak.</span></div>
        <div><strong>Service tokens</strong><span>Scoped credentials for CI and deploys, separate from any person&apos;s session.</span></div>
        <div><strong>Open source</strong><span>Use Envi Cloud, or run the whole stack on your own servers.</span></div>
      </section>

      <section className="lp-cta" id="pricing">
        <h2>Free for personal projects.<br /><span>Upgrade when the team grows.</span></h2>
        <div className="lp-hero-actions">
          <Link className="button accent large" href={start}>{user ? "Go to dashboard" : "Get started"}<ArrowRight /></Link>
          <a className="button secondary large" href={`${DOCS}/self-hosting/deployment`}>Self-host Envi</a>
        </div>
      </section>
    </main>

    <footer className="lp-footer">
      <div><Brand /><span>Secrets workflow for modern teams.</span></div>
      <nav><a href={DOCS}>Docs</a><a href={GITHUB}>GitHub</a><Link href="/auth">Sign in</Link></nav>
    </footer>
    <CookieConsent />
  </div>;
}
