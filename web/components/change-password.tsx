"use client";
import { FormEvent, useState } from "react";
import { api } from "@/lib/api";

// Changing your own password on a self-hosted instance. The current one is required.
export default function ChangePassword() {
  const [current, setCurrent] = useState(""); const [next, setNext] = useState(""); const [repeat, setRepeat] = useState("");
  const [busy, setBusy] = useState(false); const [error, setError] = useState(""); const [done, setDone] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError(""); setDone(false);
    if (next !== repeat) { setError("The new passwords don't match."); return; }
    setBusy(true);
    try {
      await api("/me/password", { method: "POST", body: JSON.stringify({ current_password: current, new_password: next }) });
      setCurrent(""); setNext(""); setRepeat(""); setDone(true);
    } catch (e) { setError((e as Error).message); }
    setBusy(false);
  }
  return <form className="password-form" onSubmit={submit}>
    <label>Current password<input required type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} /></label>
    <label>New password<input required minLength={10} type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} /></label>
    <label>Repeat new password<input required minLength={10} type="password" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} /></label>
    {error && <p className="form-error">{error}</p>}
    {done && <p className="form-success">Password changed.</p>}
    <button className="button primary" disabled={busy}>{busy ? "Saving..." : "Change password"}</button>
  </form>;
}
