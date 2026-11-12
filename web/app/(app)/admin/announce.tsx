"use client";
import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Send } from "lucide-react";

/*
  Writing to everybody.

  This form emails every subscribed person on the instance, so it is built to
  slow the sender down rather than to be quick: the recipient count is shown
  before anything is typed, and sending needs an explicit confirmation that the
  server checks too.
*/

export type Announcement = {
  id: string; subject: string; body: string; sent_by_email: string;
  recipients: number; failures: number; created_at: string;
};

export default function Announce({
  recipients, sent,
}: { recipients: number; sent: Announcement[] }) {
  const router = useRouter();
  const [pending, setPending] = useState<{ subject: string; body: string; form: HTMLFormElement } | null>(null);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const people = `${recipients.toLocaleString("en")} ${recipients === 1 ? "person" : "people"}`;

  // Submitting only opens the confirmation; the dialog does the sending.
  function review(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setError("");
    setNote("");
    setPending({ subject: String(data.get("subject") ?? "").trim(), body: String(data.get("body") ?? ""), form });
  }

  async function send() {
    if (!pending) return;
    const response = await fetch("/api/envi/admin/announcements", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ subject: pending.subject, body: pending.body, confirm: true }),
    }).catch(() => null);
    if (!response?.ok) {
      throw new Error((await response?.json().catch(() => ({})))?.error ?? "Could not send that.");
    }
    pending.form.reset();
    setPending(null);
    setNote(`Sending to ${people}. Delivery runs in the background.`);
    router.refresh();
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>Announce</h2>
        <span className="badge">{recipients.toLocaleString("en")} subscribed</span>
      </div>

      <form className="admin-announce-form" onSubmit={review}>
        {/* Every announcement opens with this, so the writer sees what the reader will. */}
        <img className="admin-announce-banner" src="/email/newsletter-banner.png" alt="The banner at the top of every announcement" />
        <label>
          <span>Subject</span>
          <input name="subject" required maxLength={120} placeholder="What changed" />
        </label>
        <label>
          <span>Message</span>
          <textarea name="body" required rows={7}
                    placeholder={"Write it as a person.\n\nBlank lines become paragraphs."} />
        </label>

        <p className="admin-announce-warn">
          <AlertTriangle />
          This goes to everyone who has not unsubscribed. There is no recall.
        </p>

        {error && <p className="form-error">{error}</p>}
        {note && <p className="admin-announce-note">{note}</p>}

        <button className="button primary" disabled={recipients === 0}>
          <Send />{`Send to ${recipients.toLocaleString("en")}`}
        </button>
      </form>

      {pending && (
        <ConfirmSend subject={pending.subject} people={people} close={() => setPending(null)} onConfirm={send} />
      )}

      {sent.length > 0 && (
        <div className="data-table admin-announce-table">
          <div className="thead"><span>Subject</span><span>Sent by</span><span>Reach</span><span>When</span></div>
          {sent.map((a) => (
            <div className="trow" key={a.id}>
              <span><strong>{a.subject}</strong></span>
              <span className="admin-dim">{a.sent_by_email || "—"}</span>
              <span>
                {a.recipients.toLocaleString("en")}
                {a.failures > 0 && <small className="admin-fail"> {a.failures} failed</small>}
              </span>
              <span className="admin-dim">{new Date(a.created_at).toLocaleDateString()}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function ConfirmSend({ subject, people, close, onConfirm }: {
  subject: string; people: string; close: () => void; onConfirm: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !busy) close(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [busy, close]);

  return (
    <div className="dialog-backdrop" onMouseDown={busy ? undefined : close}>
      <form className="dialog" onMouseDown={(e) => e.stopPropagation()} onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try { await onConfirm(); } catch (x) { setBusy(false); setError((x as Error).message); }
      }}>
        <header><h2>Send to {people}?</h2><button type="button" onClick={close} disabled={busy}>×</button></header>
        <p className="dialog-sub">
          &ldquo;{subject}&rdquo; goes to everyone who has not unsubscribed. Once it starts, it cannot be called back.
        </p>
        {error && <p className="form-error">{error}</p>}
        <div className="dialog-actions">
          <button type="button" className="button secondary" onClick={close} disabled={busy}>Keep editing</button>
          <button className="button primary" disabled={busy}>
            {busy && <span className="spinner" />}{busy ? "Sending…" : "Send it"}
          </button>
        </div>
      </form>
    </div>
  );
}
