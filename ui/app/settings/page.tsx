import Link from "next/link";
import { redirect } from "next/navigation";
import Brand from "@/components/brand";
import LogoutButton from "@/components/logout-button";
import ThemeToggle from "@/components/theme-toggle";
import { account } from "@/lib/server-api";
export default async function SettingsPage(){const user=await account();if(!user)redirect("/auth");return <div className="settings-page"><header><Brand/><div><Link className="button secondary" href="/dashboard">Back to dashboard</Link><ThemeToggle/></div></header><main><aside><a href="#account">Account</a><a href="#security">Security</a><a href="#appearance">Appearance</a></aside><div className="settings-content"><section id="account"><span className="kicker">Account</span><h1>Profile</h1><p>Your account is created and verified through email OTP.</p><label>Email address<input value={user.Email} readOnly/></label></section><section id="security"><h2>Security</h2><p>Dashboard sessions use rotating refresh tokens in secure, HTTP-only cookies. Secret values remain encrypted in Postgres.</p><div className="setting-row"><span><strong>Sign out this device</strong><small>Revokes the current refresh token.</small></span><LogoutButton/></div></section><section id="appearance"><h2>Appearance</h2><p>Choose a plain light or dark interface. Your choice stays on this device.</p><ThemeToggle/></section></div></main></div>}
