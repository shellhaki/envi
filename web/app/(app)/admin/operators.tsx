"use client";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { Megaphone, Plus, Shield, Trash2 } from "lucide-react";

/*
  Who may act on the instance.

  Two roles, and the difference is deliberately visible here rather than
  explained in a tooltip: an admin can do everything including appointing
  people, an announcer can only write to customers.
*/

export type Operator = {
  id: string; email: string; name: string; role: string; from_env: boolean;
};

export default function Operators({ operators }: { operators: Operator[] }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setBusy(true);
    setError("");
    const response = await fetch("/api/envi/admin/operators", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        email: form.get("email"),
        name: form.get("name"),
        role: form.get("role"),
      }),
    }).catch(() => null);
    setBusy(false);
    if (!response?.ok) {
      setError((await response?.json().catch(() => ({})))?.error ?? "Could not add that person.");
      return;
    }
    setOpen(false);
    router.refresh();
  }

  async function remove(operator: Operator) {
    if (!confirm(`Remove ${operator.name} (${operator.email})?`)) return;
    await fetch(`/api/envi/admin/operators/${operator.id}`, { method: "DELETE" }).catch(() => null);
    router.refresh();
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>Who can act on this instance</h2>
        <button className="button primary" onClick={() => setOpen((v) => !v)}>
          <Plus />{open ? "Cancel" : "Add operator"}
        </button>
      </div>

      {open && (
        <form className="admin-operator-form" onSubmit={add}>
          <label>
            <span>Name</span>
            <input name="name" placeholder="Ada Lovelace" required autoFocus />
          </label>
          <label>
            <span>Email</span>
            <input name="email" type="email" placeholder="ada@example.com" required />
          </label>
          <label>
            <span>Role</span>
            <select name="role" defaultValue="announcer">
              <option value="announcer">Announcer — can only announce</option>
              <option value="admin">Admin — everything, including this list</option>
            </select>
          </label>
          {error && <p className="form-error admin-operator-wide">{error}</p>}
          <button className="button primary" disabled={busy}>{busy ? "Adding…" : "Add"}</button>
        </form>
      )}

      <div className="data-table admin-operator-table">
        <div className="thead"><span>Person</span><span>Role</span><span /></div>
        {operators.map((o) => (
          <div className="trow" key={o.id || o.email}>
            <span>
              <strong>{o.name}</strong>
              <small className="admin-dim"> {o.email}</small>
            </span>
            <span className={"admin-role " + o.role}>
              {o.role === "admin" ? <Shield /> : <Megaphone />}
              {o.role}
            </span>
            {/* An ENVI_ADMINS entry has no row to delete: the only way to
                revoke one is to change the environment and redeploy. */}
            {o.from_env ? (
              <span className="badge" title="Set in ENVI_ADMINS; change the environment to revoke">env</span>
            ) : (
              <button className="icon-btn danger" title="Remove" onClick={() => remove(o)}><Trash2 /></button>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}
