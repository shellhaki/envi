import Link from "next/link";
import { redirect } from "next/navigation";
import ApiKeys from "@/components/api-keys";
import Brand from "@/components/brand";
import LogoutButton from "@/components/logout-button";
import ThemeToggle from "@/components/theme-toggle";
import ChangePassword from "@/components/change-password";
import { account, instance } from "@/lib/server-api";

export default async function SettingsPage() {
  const [user, server] = await Promise.all([account(), instance()]);
  if (!user) redirect("/auth");
  const password = server.sign_in === "password";
  return (
    <div className="settings-page">
      <header>
        <Brand />
        <div>
          <Link className="button secondary" href="/dashboard">Back to dashboard</Link>
          <ThemeToggle />
        </div>
      </header>
      <main>
        <aside>
          <a href="#account">Account</a>
          <a href="#api-keys">API keys</a>
          <a href="#security">Security</a>
          <a href="#appearance">Appearance</a>
        </aside>
        <div className="settings-content">
          <section id="account">
            <span className="kicker">Account</span>
            <h1>Profile</h1>
            <p>{password
              ? `You sign in to this self-hosted instance with a password${user.Role === "admin" ? ", as its admin" : ""}.`
              : "Your account is created and verified through email OTP."}</p>
            <label>Email address<input value={user.Email} readOnly /></label>
          </section>
          <section id="api-keys">
            <h2>API keys</h2>
            <ApiKeys />
          </section>
          <section id="security">
            <h2>Security</h2>
            {password && <div className="setting-block">
              <span><strong>Password</strong><small>At least 10 characters. Forgot it? Your admin can give you a new one.</small></span>
              <ChangePassword />
            </div>}
            <p>Dashboard sessions use rotating refresh tokens in secure, HTTP-only cookies. Secret values remain encrypted in Postgres.</p>
            <div className="setting-row">
              <span><strong>Sign out this device</strong><small>Revokes the current refresh token.</small></span>
              <LogoutButton />
            </div>
          </section>
          <section id="appearance">
            <h2>Appearance</h2>
            <p>Choose a plain light or dark interface. Your choice stays on this device.</p>
            <ThemeToggle />
          </section>
        </div>
      </main>
    </div>
  );
}
