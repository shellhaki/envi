"use client";
import { FormEvent, useState } from "react";
import { ArrowRight } from "lucide-react";
import { useRouter } from "next/navigation";

// Sign-in for a self-hosted instance: email and password, checked against the server's users file.
export default function PasswordForm({ next }: { next?: string }) {
  const router = useRouter();
  const [email, setEmail] = useState(""); const [password, setPassword] = useState(""); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      const response = await fetch("/api/auth/password", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email, password }) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) { setError(body.error || "Sign-in failed"); setBusy(false); return; }
      router.push(next || "/dashboard"); router.refresh();
    } catch {
      setError("Couldn't reach Envi. Check your connection and try again.");
      setBusy(false);
    }
  }
  return <form className="auth-form" onSubmit={submit}>
    <label>Email address<input autoFocus required type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@company.com" /></label>
    <label>Password<input required type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} /></label>
    {error && <p className="form-error">{error}</p>}
    <button className="button accent large" disabled={busy}>{busy ? "Signing in..." : "Sign in"}{busy ? <span className="spinner" /> : <ArrowRight />}</button>
  </form>;
}
