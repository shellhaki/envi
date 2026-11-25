import Image from "next/image";
import Link from "next/link";
import { Activity, ArrowRight, Check, ChevronDown, ChevronRight, Clipboard, Clock, Cloud, Eye, EyeOff, Pencil, Search, Server, Shield, Terminal, Trash2 } from "lucide-react";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import HeroHeadline from "@/components/hero-headline";
import CookieConsent from "@/components/cookie-consent";
import DashboardPreview from "@/components/landing/dashboard-preview";
import InstallTabs from "@/components/landing/install-tabs";
import { redirect } from "next/navigation";
import { instance } from "@/lib/server-api";
import { CLOUD_URL, DOCS_URL as DOCS, GITHUB_URL as GITHUB } from "@/lib/site";

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

// The CLI card: commands and what the real CLI prints back.
const LOG: [string, string][] = [
  ["envi push", "✓ Pushed 12 secrets to development"],
  ["envi pull", "✓ Wrote 12 secrets from development"],
  ["envi diff", "+ SENTRY_DSN   ~ STRIPE_SECRET_KEY"],
  ["envi run -- npm start", "Listening on http://localhost:3000"],
];

// The audit card: a few entries of the activity feed.
const TRAIL = [
  { kind: "write", icon: <Pencil />, status: "Updated", when: "Nov 24 · 09:41", chips: [["by", "diego@northwind.dev"], ["in", "staging"], ["key", "STRIPE_SECRET_KEY"]] },
  { kind: "read", icon: <Eye />, status: "Read", when: "Nov 24 · 09:38", chips: [["by", "token · deploy"], ["in", "production"]] },
  { kind: "delete", icon: <Trash2 />, status: "Deleted", when: "Nov 24 · 08:12", chips: [["by", "amara@northwind.dev"], ["in", "production"], ["key", "LEGACY_API_KEY"]] },
];

export default async function Home() {
  // A self-hosted instance has no landing page: its home is the dashboard.
  if ((await instance()).sign_in === "password") redirect("/dashboard");

  return <div className="site-page">
    <header className="site-header">
      <div className="site-header-inner">
        <Brand />
        <nav><a href="#install">Self-host</a><a href="#secrets">Product</a><a href="#pricing">Pricing</a><a href={DOCS}>Docs</a><Link href="/changelog">Changelog</Link></nav>
        <div className="header-actions">
          <ThemeToggle />
          <a className="button ghost" href={CLOUD_URL}>Envi Cloud</a>
          <a className="button primary" href="#install">Install</a>
        </div>
      </div>
    </header>

    <main>
      <section className="lp-hero">
        <div className="lp-hero-top">
          <div className="lp-hero-text">
            <a className="lp-pill" href="#install"><b>New</b>Self-host Envi with one command<ChevronRight /></a>
            <HeroHeadline />
            <div className="lp-hero-row">
              <p>Envi keeps environment variables encrypted, versioned and scoped to the people who need them, from a laptop to production. Run it on your own server for free, or let us run it on Envi Cloud.</p>
              <div className="lp-hero-actions">
                <a className="button accent large" href="#install">Self-host Envi<ArrowRight /></a>
                <a className="button secondary large" href={CLOUD_URL}>Try Envi Cloud</a>
              </div>
            </div>
          </div>
          {/* The .env being drawn into a sealed block. Decorative:
              the headline already says it in words. Two renders, one per theme,
              toggled the way the logo is. sizes collapses to almost nothing on
              narrow screens, where it is hidden, so phones download a sliver
              rather than the full image. Both load lazily, and a lazy image
              that is display: none is never fetched — so each visitor
              downloads only their own theme's render. */}
          <div className="lp-hero-art" aria-hidden="true">
            <Image className="hero-art-light" src="/hero/envi-hero-light.webp" alt="" width={1240} height={920}
                   sizes="(min-width: 960px) 520px, 1px" />
            <Image className="hero-art-dark" src="/hero/envi-hero-dark.webp" alt="" width={1240} height={920}
                   sizes="(min-width: 960px) 520px, 1px" />
          </div>
        </div>
      </section>

      <section className="lp-preview"><DashboardPreview /></section>

      <section className="lp-integrate" id="install">
        <span className="tile-icon accent"><Terminal /></span>
        <h2>Running in <span className="lp-gradient">five minutes</span></h2>
        <p>One command on a server installs the whole of Envi: the dashboard, the API and the database, with HTTPS. One more on your laptop installs the CLI. Start wherever you are.</p>
        <InstallTabs />
        <div className="lp-integrate-links">
          <a href={`${DOCS}/self-hosting`}>Self-hosting guide<ArrowRight /></a>
          <a href={`${DOCS}/installation`}>Installing the CLI<ArrowRight /></a>
        </div>
      </section>

      <section className="lp-dx">
        <h2>Made for the terminal.<br /><span>Clear to everyone else.</span></h2>
        <p>The CLI does the work where you already are. The dashboard and the audit trail show your whole team what happened, and who did it.</p>
        <div className="lp-dx-grid">
          <div className="lp-card">
            <div className="lp-card-body lp-log">
              {LOG.map(([cmd, out], i) => <div key={i}><span className="lp-log-cmd"><b>❯</b> {cmd}</span><span className="lp-log-out">{out}</span></div>)}
            </div>
            <div className="lp-card-foot"><Terminal /><span><strong>The CLI</strong>Push, pull, diff and run, in any shell and any CI.</span></div>
          </div>
          <div className="lp-card">
            <div className="lp-card-body lp-trail">
              {TRAIL.map((t, i) => <div className="lp-trail-item" key={i}>
                <span className={`lp-trail-icon ${t.kind}`}>{t.icon}</span>
                <div>
                  <div className="lp-trail-top"><span className={`lp-status ${t.kind}`}>{t.status}</span><span className="lp-when"><Clock />{t.when}</span></div>
                  <div className="lp-chips">{t.chips.map(([k, v]) => <span key={k}>{k} <b>{v}</b></span>)}</div>
                </div>
              </div>)}
            </div>
            <div className="lp-card-foot"><Activity /><span><strong>The audit trail</strong>Every change, by a person or a token, in the environment it touched.</span></div>
          </div>
        </div>
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
        <div><strong>Device sign-in</strong><span>Approve the CLI from your browser. No password or key is pasted into a terminal.</span></div>
        <div><strong>Service tokens</strong><span>Scoped credentials for CI and deploys, separate from any person&apos;s session.</span></div>
        <div><strong>Open source</strong><span>Read every line, run it on your own servers, or use Envi Cloud.</span></div>
      </section>

      <section className="lp-pricing" id="pricing">
        <div className="lp-install-head">
          <span className="kicker">Pricing</span>
          <h2>Free on your server.<br /><span>Or let us run it.</span></h2>
        </div>
        <div className="lp-install-grid">
          <div className="lp-plan">
            <span className="lp-install-icon"><Server /></span>
            <h3>Self-hosted</h3>
            <strong className="lp-price">Free</strong>
            <ul>
              <li><Check />Everything Envi does</li>
              <li><Check />Unlimited people and projects</li>
              <li><Check />Your data never leaves your server</li>
              <li><Check />Updates and backups with one command</li>
            </ul>
            <a className="button accent large" href="#install">Install it<ArrowRight /></a>
          </div>
          <div className="lp-plan">
            <span className="lp-install-icon"><Cloud /></span>
            <h3>Envi Cloud</h3>
            <strong className="lp-price">Free to start</strong>
            <ul>
              <li><Check />Nothing to run or update</li>
              <li><Check />Free for one person</li>
              <li><Check />Pay as you go, or Team, when you add people</li>
              <li><Check />Sign in with an email code</li>
            </ul>
            <a className="button secondary large" href={`${CLOUD_URL}/auth`}>Start on Envi Cloud</a>
          </div>
        </div>
      </section>
    </main>

    <footer className="lp-footer">
      <div><Brand /><span>Secrets workflow for modern teams.</span></div>
      <nav><a href={DOCS}>Docs</a><Link href="/changelog">Changelog</Link><a href={GITHUB}>GitHub</a><a href={CLOUD_URL}>Envi Cloud</a></nav>
    </footer>
    <CookieConsent />
  </div>;
}
