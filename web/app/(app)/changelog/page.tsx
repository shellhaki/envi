import type { Metadata } from "next";
import Link from "next/link";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import { CHANGELOG } from "@/lib/changelog";

const DOCS = "https://docs.envisecrets.com";
const GITHUB = "https://github.com/shellhaki/envi";

export const metadata: Metadata = {
  title: "Changelog · Envi",
  description: "What changed in Envi, by date.",
};

// inline turns `code` spans in a changelog line into <code>.
function inline(text: string) {
  return text.split(/(`[^`]+`)/).map((part, i) =>
    part.startsWith("`") && part.endsWith("`") ? <code key={i}>{part.slice(1, -1)}</code> : part);
}

function longDate(date: string) {
  return new Date(`${date}T00:00:00Z`).toLocaleDateString("en-GB", { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
}

export default function Changelog() {
  return <div className="site-page">
    <header className="site-header">
      <div className="site-header-inner">
        <Brand />
        <nav><Link href="/">Home</Link><a href={DOCS}>Docs</a><a href={GITHUB}>GitHub</a></nav>
        <div className="header-actions"><ThemeToggle /><Link className="button primary" href="/auth">Sign in</Link></div>
      </div>
    </header>

    <main className="changelog">
      <div className="changelog-head">
        <span className="kicker">Changelog</span>
        <h1>What&rsquo;s new in Envi</h1>
        <p>Every change you&rsquo;d notice, newest first. Update the CLI with <code>envi update now</code>.</p>
      </div>

      {CHANGELOG.map((entry) => <article className="changelog-entry" id={entry.date} key={entry.date + entry.title}>
        <aside>
          <a href={`#${entry.date}`}><time dateTime={entry.date}>{longDate(entry.date)}</time></a>
          {entry.version && <span className="badge">{entry.version}</span>}
        </aside>
        <div>
          <h2>{entry.title}</h2>
          <p>{inline(entry.summary)}</p>
          <ul>{entry.changes.map((change) => <li key={change}>{inline(change)}</li>)}</ul>
        </div>
      </article>)}
    </main>

    <footer className="lp-footer">
      <div><Brand /><span>Secrets workflow for modern teams.</span></div>
      <nav><a href={DOCS}>Docs</a><a href={GITHUB}>GitHub</a><Link href="/auth">Sign in</Link></nav>
    </footer>
  </div>;
}
