import { cookies } from "next/headers";
import { notFound } from "next/navigation";
import Link from "next/link";
import { Activity, Boxes, FolderGit2, GitBranch, KeyRound, Layers, ShieldCheck, Users, Wallet } from "lucide-react";
import { upstream } from "@/lib/server-api";
import { readSession, sessionCookie } from "@/lib/web-session";
import { relativeTime } from "@/app/utils";
import Growth from "./growth";
import Expenses, { money } from "./expenses";
import Live from "./live";
import Operators, { type Operator } from "./operators";
import Announce, { type Announcement } from "./announce";

/*
  The operator view: the whole instance in one screen.

  Rendered on the server and never handed to the client unless the API has
  already said this session is an operator. A client-side check would mean the
  numbers crossed the wire first and the guard only decided whether to paint
  them, which is not a guard.

  Anything other than a clean 200 becomes a 404. A signed-in user who is not an
  operator sees exactly what a stranger sees.
*/

export const dynamic = "force-dynamic";

type Totals = {
  users: number; organizations: number; team_orgs: number; projects: number;
  environments: number; production_envs: number; secrets: number;
  secret_versions: number; kv_entries: number; grants: number;
  pending_invites: number; service_tokens: number; api_keys: number;
};
type Recent = {
  new_users: number; new_projects: number; secret_writes: number;
  secret_reads: number; active_orgs: number; active_people: number;
};
type Revenue = {
  available: boolean; note?: string; mrr: number;
  paid_orgs: number; free_orgs: number; trial_orgs: number;
  team_orgs: number; payg_orgs: number; failed_charges: number;
  collected: { currency: string; this_month_minor: number; all_time_minor: number }[];
};
type Signup = { email: string; name: string; created_at: string; projects: number; secrets: number };
type CurrencyTotal = { currency: string; total_minor: number; monthly_minor: number; count: number };
type CategoryTotal = { category: string; currency: string; total_minor: number; count: number };
type Expense = {
  id: string; amount_minor: number; currency: string; category: string;
  description: string; spent_on: string; recurring: string;
};
type Snapshot = {
  generated_at: string; mode: string; currency: string;
  totals: Totals; last_7_days: Recent; last_30_days: Recent;
  revenue: Revenue;
  spending: { totals: CurrencyTotal[]; categories: CategoryTotal[]; recent: Expense[] };
  signups: Signup[]; growth: { day: string; users: number }[];
};

type Who = { role: string; announce: boolean; manage: boolean };

/*
  One fetch helper for every operator endpoint.

  Each returns null rather than throwing on a refusal, because an announcer is
  *expected* to be refused the figures: that is the role working, not an error,
  and the page renders what they may see instead of failing.
*/
async function fromAPI<T>(path: string, access: string): Promise<T | null> {
  const response = await upstream(path, { headers: { Authorization: `Bearer ${access}` } }).catch(() => null);
  if (!response?.ok) return null;
  return response.json() as Promise<T>;
}

async function load() {
  const session = readSession((await cookies()).get(sessionCookie)?.value);
  if (!session) notFound();

  const who = await fromAPI<Who>("/admin/whoami", session.access);
  // Not an operator at all. Same answer a stranger gets.
  if (!who) notFound();

  // In parallel: three round trips in sequence would make the poll interval
  // visible as a stutter.
  const [stats, operators, announcements] = await Promise.all([
    who.manage ? fromAPI<Snapshot>("/admin/stats", session.access) : null,
    who.manage ? fromAPI<{ operators: Operator[] }>("/admin/operators", session.access) : null,
    who.announce
      ? fromAPI<{ announcements: Announcement[]; recipients: number }>("/admin/announcements", session.access)
      : null,
  ]);
  return { who, stats, operators: operators?.operators ?? [], announcements };
}

const nf = new Intl.NumberFormat("en");

export default async function AdminPage() {
  const { who, stats, operators, announcements } = await load();

  // An announcer gets the one panel they are here for, and nothing else.
  if (!who.manage) {
    return (
      <main className="admin-page">
        <div className="page-title">
          <div>
            <span className="eyebrow">Announcer</span>
            <h1>Announce</h1>
            <p>Write to everyone who has not unsubscribed.</p>
          </div>
          <Link className="button secondary" href="/dashboard">Back to workspace</Link>
        </div>
        {announcements && (
          <Announce recipients={announcements.recipients} sent={announcements.announcements} />
        )}
      </main>
    );
  }

  if (!stats) notFound();
  const data = stats;
  const { totals: t, last_7_days: week, last_30_days: month, revenue } = data;
  const selfHosted = data.mode === "self-hosted";
  // The first currency with a running cost is the one the headline card shows.
  // Two currencies are reported separately in the panel below rather than
  // summed, because there is no exchange rate in this product.
  const cost = data.spending.totals.find((x) => x.monthly_minor > 0) ?? data.spending.totals[0];
  // This month's money, per currency, never summed across currencies: pay as
  // you go actually collected, plus what Team subscriptions bring in.
  const earned = [
    ...revenue.collected.filter((c) => c.this_month_minor > 0).map((c) => money(c.this_month_minor, c.currency)),
    ...(revenue.mrr > 0 ? [`$${nf.format(revenue.mrr)} Team`] : []),
  ];

  return (
    <main className="admin-page">
      <div className="page-title">
        <div>
          <span className="eyebrow">Operator</span>
          <h1>Instance</h1>
          <p>Every organization, every project, and what it costs to run.</p>
        </div>
        <div className="admin-title-actions">
          <Live generatedAt={data.generated_at} />
          <Link className="button secondary" href="/dashboard">Back to workspace</Link>
        </div>
      </div>

      <section className="stat-cards">
        <Stat label="Users" value={t.users} icon={<Users />} note={`${plus(month.new_users)} in 30 days`} />
        <Stat label="Projects" value={t.projects} icon={<FolderGit2 />} note={`${plus(month.new_projects)} in 30 days`} />
        <Stat label="Secrets stored" value={t.secrets} icon={<KeyRound />} note={`${nf.format(t.secret_versions)} versions`} />
        <Stat label={selfHosted ? "Running cost" : "Revenue"}
              value={selfHosted
                ? (cost ? money(cost.monthly_minor, cost.currency) : "—")
                : (revenue.available ? (earned.length ? earned.join(" · ") : "$0") : "—")}
              icon={<Wallet />}
              note={selfHosted
                ? "per month"
                : (revenue.available
                  ? `this month · ${nf.format(revenue.team_orgs)} Team, ${nf.format(revenue.payg_orgs)} pay as you go`
                  : "billing tables missing")} />
      </section>

      {/* A self-hosted instance has nobody to charge, so an empty revenue
          card there is correct rather than a missing feature. Only the hosted
          deployment is told when its billing tables are missing. */}
      {!selfHosted && !revenue.available && (
        <p className="admin-note">
          <ShieldCheck />
          {revenue.note}
        </p>
      )}
      {!selfHosted && revenue.available && revenue.failed_charges > 0 && (
        <p className="admin-note">
          <ShieldCheck />
          {nf.format(revenue.failed_charges)} pay as you go {revenue.failed_charges === 1 ? "charge has" : "charges have"} been
          refused and {revenue.failed_charges === 1 ? "is" : "are"} waiting for a retry.
        </p>
      )}
      {selfHosted && (
        <p className="admin-note">
          <ShieldCheck />
          Self-hosted instance. No billing, no quotas, nothing reported as revenue.
        </p>
      )}

      <section className="admin-split">
        <div className="panel">
          <div className="panel-head"><h2>Signups, last 30 days</h2></div>
          <Growth points={data.growth} />
        </div>

        <div className="panel">
          <div className="panel-head"><h2>Activity</h2></div>
          <dl className="admin-rows">
            {/* Headed rather than footnoted: two bare numbers side by side are
                unreadable without being told which window each one is. */}
            <div className="admin-row admin-row-head">
              <dt>Window</dt>
              <dd><span>7 days</span><span>30 days</span></dd>
            </div>
            <Row label="Secret reads" week={week.secret_reads} month={month.secret_reads} />
            <Row label="Secret writes" week={week.secret_writes} month={month.secret_writes} />
            <Row label="Active orgs" week={week.active_orgs} month={month.active_orgs} />
            <Row label="Active people" week={week.active_people} month={month.active_people} />
          </dl>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h2>Everything else</h2></div>
        <div className="admin-grid">
          <Small label="Organizations" value={t.organizations} icon={<Boxes />} />
          <Small label="Team orgs" value={t.team_orgs} icon={<Users />} />
          <Small label="Environments" value={t.environments} icon={<Layers />} />
          <Small label="Production envs" value={t.production_envs} icon={<GitBranch />} />
          <Small label="Access grants" value={t.grants} icon={<ShieldCheck />} />
          <Small label="Pending invites" value={t.pending_invites} icon={<Activity />} />
          <Small label="Service tokens" value={t.service_tokens} icon={<KeyRound />} />
          <Small label="Personal API keys" value={t.api_keys} icon={<KeyRound />} />
        </div>
      </section>

      <Expenses
        totals={data.spending.totals}
        categories={data.spending.categories}
        recent={data.spending.recent}
        defaultCurrency={data.currency}
      />

      {announcements && (
        <Announce recipients={announcements.recipients} sent={announcements.announcements} />
      )}

      <Operators operators={operators} />

      <section className="panel">
        <div className="panel-head">
          <h2>Who signed up</h2>
          <span className="badge">{nf.format(t.users)} total</span>
        </div>
        {data.signups.length ? (
          <div className="data-table admin-table">
            <div className="thead"><span>Email</span><span>Joined</span><span>Projects</span><span>Secrets</span></div>
            {data.signups.slice(0, 25).map((person) => (
              <div className="trow" key={person.email}>
                <code>{person.email}</code>
                <span className="admin-dim">{relativeTime(person.created_at)}</span>
                {/* A signup that never made a project is the number that
                    matters most early, and it hides inside every total. */}
                <span className={person.projects ? "" : "admin-zero"}>{nf.format(person.projects)}</span>
                <span className={person.secrets ? "" : "admin-zero"}>{nf.format(person.secrets)}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="admin-foot">Nobody yet.</p>
        )}
      </section>

      <p className="admin-foot">
        Showing the {Math.min(25, data.signups.length)} most recent of {nf.format(t.users)}.
        Every number is from the same instant.
      </p>
    </main>
  );
}

function plus(n: number) { return n > 0 ? `+${nf.format(n)}` : nf.format(n); }

function Stat({ label, value, icon, note }: { label: string; value: number | string; icon: React.ReactNode; note: string }) {
  return (
    <div className="stat-card">
      <span className="stat-icon">{icon}</span>
      <span>{label}</span>
      <strong>{typeof value === "number" ? nf.format(value) : value}</strong>
      <small className="admin-dim">{note}</small>
    </div>
  );
}

function Small({ label, value, icon }: { label: string; value: number; icon: React.ReactNode }) {
  return (
    <div className="admin-small">
      <span className="admin-small-icon">{icon}</span>
      <div>
        <strong>{nf.format(value)}</strong>
        <span>{label}</span>
      </div>
    </div>
  );
}

function Row({ label, week, month }: { label: string; week: number; month: number }) {
  return (
    <div className="admin-row">
      <dt>{label}</dt>
      <dd><span>{nf.format(week)}</span><span>{nf.format(month)}</span></dd>
    </div>
  );
}
