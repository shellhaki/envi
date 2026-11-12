"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, CreditCard, Folder, KeyRound, LayoutDashboard, UserCog, Users } from "lucide-react";

const items = [
  { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
  { href: "/dashboard/secrets", label: "Secrets", icon: KeyRound },
  { href: "/dashboard/projects", label: "Projects", icon: Folder },
  { href: "/dashboard/sharing", label: "Sharing", icon: Users },
  { href: "/dashboard/activity", label: "Activity", icon: Activity },
  { href: "/dashboard/billing", label: "Billing", icon: CreditCard },
];

// billing is false on a server with no billing; admin adds the Users page of a self-hosted instance.
export default function SidebarNav({ billing = true, admin = false, children }: { billing?: boolean; admin?: boolean; children?: React.ReactNode }) {
  const pathname = usePathname();
  const shown = [
    ...(billing ? items : items.filter((item) => item.href !== "/dashboard/billing")),
    ...(admin ? [{ href: "/dashboard/users", label: "Users", icon: UserCog }] : []),
  ];
  return (
    <nav className="sidebar-nav">
      <span className="sidebar-label">Workspace</span>
      {shown.map(({ href, label, icon: Icon }) => {
        // "/dashboard" must match exactly; the rest match their subtree.
        const active = href === "/dashboard" ? pathname === href : pathname.startsWith(href);
        return (
          <Link key={href} href={href} className={active ? "active" : undefined} aria-current={active ? "page" : undefined} title={label}>
            <Icon />
            <span className="nav-label">{label}</span>
          </Link>
        );
      })}
      {/* Server-rendered, and absent entirely for anyone who is not an
          operator — see components/operator-link.tsx. */}
      {children}
    </nav>
  );
}
