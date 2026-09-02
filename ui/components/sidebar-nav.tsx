"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, Folder, KeyRound, LayoutDashboard, Users } from "lucide-react";

const items = [
  { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
  { href: "/dashboard/secrets", label: "Secrets", icon: KeyRound },
  { href: "/dashboard/projects", label: "Projects", icon: Folder },
  { href: "/dashboard/sharing", label: "Sharing", icon: Users },
  { href: "/dashboard/activity", label: "Activity", icon: Activity },
];

export default function SidebarNav() {
  const pathname = usePathname();
  return (
    <nav className="sidebar-nav">
      <span className="sidebar-label">Workspace</span>
      {items.map(({ href, label, icon: Icon }) => {
        // "/dashboard" must match exactly; the rest match their subtree.
        const active = href === "/dashboard" ? pathname === href : pathname.startsWith(href);
        return (
          <Link key={href} href={href} className={active ? "active" : undefined} aria-current={active ? "page" : undefined}>
            <Icon />
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
