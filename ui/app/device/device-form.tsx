"use client";
import { FormEvent, useState } from "react";
import { ArrowRight } from "lucide-react";
export default function DeviceForm({ initialCode }: { initialCode: string }) {
  const [code, setCode] = useState(initialCode); const [busy, setBusy] = useState(false); const [error, setError] = useState(""); const [done, setDone] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    // The catch-all proxy attaches this session's Bearer token to the upstream call.
    const response = await fetch("/api/envi/auth/device/approve", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ user_code: code }) });
    setBusy(false);
    if (!response.ok) { const body = await response.json().catch(() => ({})); setError(body.error || "That code is invalid or expired."); return; }
    setDone(true);
  }
  if (done) return <div className="auth-form"><span className="kicker">Connected</span><p>Your terminal is authorized. You can close this tab and return to it.</p></div>;
  return <form className="auth-form" onSubmit={submit}><label>One-time code<input autoFocus required type="text" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="WXYZ-ABCD" /></label>{error && <p className="form-error">{error}</p>}<button className="button primary large" disabled={busy}>{busy ? "Authorizing..." : "Authorize device"}<ArrowRight /></button></form>;
}
