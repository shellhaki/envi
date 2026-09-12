"use client";
import { FormEvent, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { ArrowRight, ShieldCheck } from "lucide-react";
import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";

type Preview = { Email: string; ProjectName: string; EnvironmentName: string; Permission: string; ExpiresAt: string };
// "checking" covers both the initial preview load and the accept attempt that
// follows it — a visitor who is already signed in as the invited address
// never sees an intermediate state, they just land on "accepted".
type Phase = "checking" | "invalid" | "needs-code" | "mismatch" | "accepted" | "error";

async function api(path: string, init: RequestInit = {}) {
  const r = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...init.headers } });
  const body = await r.json().catch(() => ({}));
  return { ok: r.ok, status: r.status, body };
}

export default function InvitePage() {
  const { token } = useParams<{ token: string }>();
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("checking");
  const [preview, setPreview] = useState<Preview>();
  const [sent, setSent] = useState(false);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const p = await api(`/api/envi/invitations/${token}`);
      if (cancelled) return;
      if (!p.ok) { setPhase("invalid"); return; }
      setPreview(p.body);
      // Signed in already? This resolves instantly for the common case — the
      // same browser that requested the invite, or someone who was already
      // logged in as the invited address.
      const accept = await api("/api/envi/invitations/accept", { method: "POST", body: JSON.stringify({ token }) });
      if (cancelled) return;
      if (accept.ok) { setPhase("accepted"); return; }
      if (accept.status === 401) { setPhase("needs-code"); return; }
      if (accept.status === 403) { setPhase("mismatch"); return; }
      setPhase("error");
    })();
    return () => { cancelled = true; };
  }, [token]);

  async function requestCode() {
    setBusy(true); setError("");
    const r = await api("/api/auth/request", { method: "POST", body: JSON.stringify({ email: preview?.Email }) });
    setBusy(false);
    if (!r.ok) { setError(r.body.error || "Could not send a code"); return; }
    setSent(true);
  }
  async function verifyAndAccept(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    const v = await api("/api/auth/verify", { method: "POST", body: JSON.stringify({ email: preview?.Email, code }) });
    if (!v.ok) { setBusy(false); setError(v.body.error || "Invalid or expired code"); return; }
    const accept = await api("/api/envi/invitations/accept", { method: "POST", body: JSON.stringify({ token }) });
    setBusy(false);
    if (!accept.ok) { setError("Signed in, but the invitation could not be accepted — it may have expired."); return; }
    setPhase("accepted");
  }
  async function switchAccount() {
    setBusy(true);
    await api("/api/auth/logout", { method: "POST" });
    setBusy(false);
    setPhase("needs-code");
  }

  return <main className="auth-page">
    <header><Brand /><ThemeToggle /></header>
    <section className="auth-box">
      {phase === "checking" && <><span className="kicker">Invitation</span><h1>Checking your invite&hellip;</h1></>}

      {phase === "invalid" && <><span className="kicker">Invitation</span><h1>This invite isn&rsquo;t valid</h1><p>It may have already been accepted, revoked, or it&rsquo;s expired. Ask whoever invited you to send a new one.</p></>}

      {phase === "error" && <><span className="kicker">Invitation</span><h1>Something went wrong</h1><p>We couldn&rsquo;t accept this invitation just now. Try opening the link again.</p></>}

      {phase === "accepted" && preview && <>
        <span className="kicker">Invitation accepted</span>
        <h1>You&rsquo;re in on {preview.ProjectName}</h1>
        <p>You have {preview.Permission} access{preview.EnvironmentName ? ` to ${preview.EnvironmentName}` : ""}.</p>
        <button className="button primary large" onClick={() => router.push("/dashboard")}>Go to dashboard<ArrowRight /></button>
      </>}

      {phase === "mismatch" && preview && <>
        <span className="kicker">Wrong account</span>
        <h1>This invite is for {preview.Email}</h1>
        <p>You&rsquo;re signed in with a different account. Sign out and continue with {preview.Email} to accept it.</p>
        <button className="button primary large" disabled={busy} onClick={switchAccount}>{busy ? "Working..." : "Sign out and continue"}</button>
      </>}

      {phase === "needs-code" && preview && <>
        <span className="kicker">You&rsquo;ve been invited</span>
        <h1>Join {preview.ProjectName} on Envi</h1>
        <p className="invite-summary"><ShieldCheck /> {preview.Permission} access{preview.EnvironmentName ? ` · ${preview.EnvironmentName}` : ""}</p>
        <p>We&rsquo;ll send a one-time code to <strong>{preview.Email}</strong>{sent ? "" : " to confirm it&rsquo;s you"}. If you don&rsquo;t have an Envi account yet, this creates one.</p>
        {!sent
          ? <button className="button primary large" disabled={busy} onClick={requestCode}>{busy ? "Sending..." : "Send code"}<ArrowRight /></button>
          : <form className="auth-form" onSubmit={verifyAndAccept}>
              <label>One-time code<input autoFocus required inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" /></label>
              {error && <p className="form-error">{error}</p>}
              <button className="button primary large" disabled={busy}>{busy ? "Working..." : "Verify and join"}<ArrowRight /></button>
            </form>}
        {!sent && error && <p className="form-error">{error}</p>}
      </>}
    </section>
  </main>;
}
