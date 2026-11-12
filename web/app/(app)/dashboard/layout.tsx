import Image from "next/image";
import Link from "next/link";
import { redirect } from "next/navigation";
import Brand from "@/components/brand";
import LogoutButton from "@/components/logout-button";
import OperatorLink from "@/components/operator-link";
import SidebarNav from "@/components/sidebar-nav";
import SidebarToggle from "@/components/sidebar-toggle";
import ThemeToggle from "@/components/theme-toggle";
import { account, firstName, instance } from "@/lib/server-api";
import { initials } from "@/lib/utils";
import Workspace from "./workspace";

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const [user, server] = await Promise.all([account(), instance()]);
  if (!user) redirect("/auth");
  const admin = user.Role === "admin";
  return (
    <div className="product-shell">
      <aside className="product-sidebar">
        <div className="sidebar-head">
          <Brand />
          <Image className="brand-mark" src="/envi-icon.png" alt="" width={30} height={30} />
        </div>
        <SidebarNav billing={server.billing} admin={admin}>
          <OperatorLink />
        </SidebarNav>
        <div className="sidebar-foot">
          <Link className="user-chip" href="/settings" title={user.Email}>
            <span className="avatar">{initials(user.Email)}</span>
            <span className="user-meta"><strong>{firstName(user.Email)}</strong><small>{user.Email}</small></span>
          </Link>
          <LogoutButton />
        </div>
        <SidebarToggle />
      </aside>
      <div className="product-main">
        <header className="product-topbar">
          <span className="topbar-workspace">{firstName(user.Email)}&rsquo;s workspace</span>
          <ThemeToggle />
        </header>
        {/* Pages render nothing; Workspace reads the path and persists across them. */}
        {children}
        <Workspace name={firstName(user.Email)} instance={server.sign_in === "password"} admin={admin} />
      </div>
    </div>
  );
}
