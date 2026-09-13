"use client";
import { ChevronLeft } from "lucide-react";

/**
 * Collapses the sidebar to an icon rail. State lives on the <html> element
 * and in localStorage, matching ThemeToggle — an inline script in the root
 * layout applies it before first paint so a collapsed rail never flashes
 * open on load. The arrow flips via CSS off that same attribute.
 */
export default function SidebarToggle() {
  function toggle() {
    const collapsed = document.documentElement.dataset.sidebar === "collapsed";
    const next = collapsed ? "expanded" : "collapsed";
    document.documentElement.dataset.sidebar = next;
    try { localStorage.setItem("envi_sidebar", next); } catch {}
  }
  return (
    <button className="sidebar-edge-toggle" onClick={toggle} aria-label="Toggle sidebar" title="Toggle sidebar">
      <ChevronLeft />
    </button>
  );
}
