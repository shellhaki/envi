import Link from "next/link";
import { Activity, Folder, KeyRound, LayoutDashboard, Settings, Users } from "lucide-react";
import { redirect } from "next/navigation";
import Brand from "@/components/brand";
import LogoutButton from "@/components/logout-button";
import ThemeToggle from "@/components/theme-toggle";
import { account, firstName } from "@/lib/server-api";
export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const user = await account(); if (!user) redirect("/auth");
  return <div className="product-shell"><aside className="product-sidebar"><Brand /><nav><Link href="/dashboard"><LayoutDashboard />Overview</Link><Link href="/dashboard/secrets"><KeyRound />Secrets</Link><Link href="/dashboard/projects"><Folder />Projects</Link><Link href="/dashboard/sharing"><Users />Sharing</Link><Link href="/dashboard/activity"><Activity />Activity</Link></nav><div className="sidebar-account"><Link href="/settings"><Settings />Settings</Link><LogoutButton /></div></aside><div className="product-main"><header className="product-topbar"><div><strong>{firstName(user.Email)}</strong><span>{user.Email}</span></div><ThemeToggle /></header>{children}</div></div>;
}
