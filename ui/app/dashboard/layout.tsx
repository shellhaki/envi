import Link from "next/link";
import { redirect } from "next/navigation";
import Brand from "@/components/brand";
import LogoutButton from "@/components/logout-button";
import SidebarNav from "@/components/sidebar-nav";
import ThemeToggle from "@/components/theme-toggle";
import { account, firstName } from "@/lib/server-api";
import { initials } from "@/app/utils";

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const user = await account();
  if (!user) redirect("/auth");
  return (
    <div className="product-shell">
      <aside className="product-sidebar">
        <div className="sidebar-head"><Brand /></div>
        <SidebarNav />
        <div className="sidebar-foot">
          <Link className="user-chip" href="/settings">
            <span className="avatar">{initials(user.Email)}</span>
            <span className="user-meta"><strong>{firstName(user.Email)}</strong><small>{user.Email}</small></span>
          </Link>
          <LogoutButton />
        </div>
      </aside>
      <div className="product-main">
        <header className="product-topbar">
          <span className="topbar-workspace">{firstName(user.Email)}&rsquo;s workspace</span>
          <ThemeToggle />
        </header>
        {children}
      </div>
    </div>
  );
}
