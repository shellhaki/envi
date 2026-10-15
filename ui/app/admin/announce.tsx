"use client";
import { FormEvent, useState } from "react";
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const subject = String(data.get("subject") ?? "").trim();
    if (!confirm(`Send "${subject}" to ${recipients} ${recipients === 1 ? "person" : "people"}? This cannot be taken back.`)) return;

    setBusy(true);
    setError("");
    setNote("");
    const response = await fetch("/api/envi/admin/announcements", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ subject, body: data.get("body"), confirm: true }),
    }).catch(() => null);
    setBusy(false);

    if (!response?.ok) {
      setError((await response?.json().catch(() => ({})))?.error ?? "Could not send that.");
      return;
    }
    form.reset();
    setNote(`Sending to ${recipients} ${recipients === 1 ? "person" : "people"}. Delivery runs in the background.`);
    router.refresh();
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>Announce</h2>
        <span className="badge">{recipients.toLocaleString("en")} subscribed</span>
      </div>

      <form className="admin-announce-form" onSubmit={send}>
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

        <button className="button primary" disabled={busy || recipients === 0}>
          <Send />{busy ? "Sending…" : `Send to ${recipients.toLocaleString("en")}`}
        </button>
      </form>

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
