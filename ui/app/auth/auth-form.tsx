"use client";
import { FormEvent, useState } from "react";
import { ArrowRight } from "lucide-react";
import { useRouter } from "next/navigation";
export default function AuthForm({ next }: { next?: string }) {
  const router = useRouter();
  const [email, setEmail] = useState(""); const [code, setCode] = useState(""); const [sent, setSent] = useState(false); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError("");
    const response = await fetch(sent ? "/api/auth/verify" : "/api/auth/request", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(sent ? { email, code } : { email }) });
    const body = await response.json().catch(() => ({})); setBusy(false);
    if (!response.ok) { setError(body.error || "Request failed"); return; }
    if (sent) { router.push(next || "/dashboard"); router.refresh(); } else setSent(true);
  }
  return <form className="auth-form" onSubmit={submit}><label>{sent ? "One-time code" : "Email address"}<input autoFocus required type={sent ? "text" : "email"} inputMode={sent ? "numeric" : "email"} autoComplete={sent ? "one-time-code" : "email"} value={sent ? code : email} onChange={(e) => sent ? setCode(e.target.value) : setEmail(e.target.value)} placeholder={sent ? "123456" : "you@company.com"} /></label>{error && <p className="form-error">{error}</p>}<button className="button primary large" disabled={busy}>{busy ? "Working..." : sent ? "Verify and continue" : "Send login code"}<ArrowRight /></button>{sent && <button type="button" className="link-button" onClick={() => { setSent(false); setCode(""); }}>Use another email</button>}</form>;
}
