import Image from "next/image";
import { Activity, ChevronDown, Clipboard, CreditCard, Download, Eye, EyeOff, Folder, KeyRound, LayoutDashboard, Pencil, Plus, Search, Users } from "lucide-react";

// A still of the Envi Cloud dashboard, built from markup rather than a screenshot so it stays sharp,
// follows the theme, and changes when the dashboard does. It is decoration: inert and hidden from
// screen readers, since the page says the same in words.

const NAV = [
  [{ label: "Overview", icon: LayoutDashboard }, { label: "Secrets", icon: KeyRound, active: true }, { label: "Projects", icon: Folder }],
  [{ label: "Sharing", icon: Users }, { label: "Activity", icon: Activity }],
  [{ label: "Billing", icon: CreditCard }],
];

const SECRETS: [string, string | null][] = [
  ["DATABASE_URL", null],
  ["REDIS_URL", "redis://cache.internal:6379/0"],
  ["RESEND_API_KEY", null],
  ["SESSION_SECRET", null],
  ["STRIPE_SECRET_KEY", null],
  ["STRIPE_WEBHOOK_SECRET", null],
  ["SENTRY_DSN", null],
];

const STATS = [["Keys", "12"], ["Revision", "42"], ["Last change", "2 min ago"], ["Reads today", "318"]];

export default function DashboardPreview() {
  return <div className="dp-stage" aria-hidden="true" inert>
    <div className="dp-window">
      <aside className="dp-side">
        <div className="dp-brand"><Image src="/envi-icon.png" alt="" width={26} height={26} /><strong>northwind</strong><ChevronDown /></div>
        {NAV.map((group, i) => <div className="dp-group" key={i}>
          {group.map(({ label, icon: Icon, active }) => <span key={label} className={active ? "active" : undefined}><Icon />{label}</span>)}
        </div>)}
        <div className="dp-user"><span className="avatar">AM</span><span><strong>amara</strong><small>amara@northwind.dev</small></span></div>
      </aside>
      <div className="dp-main">
        <div className="dp-title">
          <div><h3>Secrets</h3><p>northwind-api</p></div>
          <div className="dp-actions"><span className="button primary"><Plus />Add secret</span><span className="icon-button"><Download /></span></div>
        </div>
        <div className="dp-tabs"><span>development</span><span>staging</span><span className="active prod">production</span></div>
        <div className="dp-stats">{STATS.map(([k, v]) => <div key={k}><small>{k}</small><strong>{v}</strong></div>)}</div>
        <div className="dp-toolbar">
          <span className="dp-input"><Search />Search keys…</span>
          <span className="dp-input dp-select">All keys<ChevronDown /></span>
        </div>
        <div className="panel">
          <div className="data-table">
            <div className="thead"><span>Key</span><span>Value</span><span /></div>
            {SECRETS.map(([key, shown]) => <div className="trow" key={key}>
              <code>{key}</code>
              <code className="val">{shown ?? "•".repeat(14)}</code>
              <div className="cell-actions"><span className="icon-btn">{shown ? <EyeOff /> : <Eye />}</span><span className="icon-btn"><Clipboard /></span><span className="icon-btn"><Pencil /></span></div>
            </div>)}
          </div>
        </div>
      </div>
    </div>
  </div>;
}
