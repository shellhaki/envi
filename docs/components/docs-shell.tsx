"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, ArrowRight, Menu, X } from "lucide-react";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import { SITE_URL, docsNav, findDoc } from "@/lib/docs-nav";

type Heading = { id: string; text: string };

export default function DocsShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const doc = findDoc(pathname);
  const [navOpen, setNavOpen] = useState(false);
  const [headings, setHeadings] = useState<Heading[]>([]);

  // Build the "on this page" list from what actually rendered, rather than
  // duplicating each page's outline in the nav config. The headings are
  // external (DOM) state, so this reads them after paint rather than setting
  // state synchronously in the effect body.
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const found: Heading[] = [];
      document.querySelectorAll<HTMLHeadingElement>(".doc-body h2").forEach((h) => {
        if (!h.id) h.id = h.textContent?.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") ?? "";
        found.push({ id: h.id, text: h.textContent ?? "" });
      });
      setHeadings(found);
    });
    return () => cancelAnimationFrame(frame);
  }, [pathname]);

  const sidebar = (
    <nav className="docs-nav">
      {docsNav.map((group) => (
        <div className="docs-nav-group" key={group.group}>
          <span className="docs-nav-label">{group.group}</span>
          {group.pages.map((page) => (
            // Closing on click, rather than in an effect watching the route:
            // the drawer only ever opens from this nav, so this is the one
            // place it needs to close.
            <Link
              key={page.slug}
              href={page.slug}
              className={page.slug === doc?.page.slug ? "active" : undefined}
              onClick={() => setNavOpen(false)}
            >
              {page.title}
            </Link>
          ))}
        </div>
      ))}
    </nav>
  );

  return (
    <div className="docs-page">
      <header className="docs-topbar">
        <div className="docs-topbar-left">
          <button className="docs-menu-button" onClick={() => setNavOpen((o) => !o)} aria-label="Toggle documentation menu">
            {navOpen ? <X /> : <Menu />}
          </button>
          <Brand />
          <span className="docs-badge">Docs</span>
        </div>
        <div className="docs-topbar-right">
          <a className="docs-topbar-link" href={SITE_URL}>Home</a>
          <a className="docs-topbar-link" href="https://github.com/shellhaki/envi" target="_blank" rel="noreferrer">GitHub</a>
          <ThemeToggle />
          <a className="button accent" href={`${SITE_URL}/dashboard`}>Dashboard</a>
        </div>
      </header>

      <div className="docs-shell">
        <aside className={`docs-sidebar${navOpen ? " open" : ""}`}>{sidebar}</aside>

        <main className="docs-main">
          <article className="doc-body">
            {doc && (
              <div className="doc-header">
                <h1>{doc.page.title}</h1>
                <p>{doc.page.description}</p>
              </div>
            )}
            {children}
          </article>

          {doc && (doc.prev || doc.next) && (
            <nav className="doc-pager">
              {doc.prev
                ? <Link className="doc-pager-link prev" href={doc.prev.slug}><ArrowLeft /><span><small>Previous</small>{doc.prev.title}</span></Link>
                : <span />}
              {doc.next && <Link className="doc-pager-link next" href={doc.next.slug}><span><small>Next</small>{doc.next.title}</span><ArrowRight /></Link>}
            </nav>
          )}
        </main>

        <aside className="docs-toc">
          {headings.length > 0 && (
            <>
              <span className="docs-nav-label">On this page</span>
              {headings.map((h) => <a key={h.id} href={`#${h.id}`}>{h.text}</a>)}
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
