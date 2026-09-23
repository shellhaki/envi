"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { Check, Clipboard, KeyRound, Plus, Trash2 } from "lucide-react";
import Loader from "@/components/loader";
import Select from "@/components/select";
import Toast from "@/components/toast";
import { api } from "@/lib/api";

type ApiKey = {
  id: string;
  name: string;
  permission: string;
  secret?: string;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
};

const PERMISSIONS = [
  { value: "read", label: "Read — view secrets" },
  { value: "write", label: "Write — view and change secrets" },
  { value: "manage", label: "Manage — everything you can do" },
];

const EXPIRY = [
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
  { value: "0", label: "Never expires" },
];

/**
 * Create, list and revoke personal API keys.
 *
 * The secret is returned once, by the only response that will ever contain it,
 * so a freshly created key stays on screen until it is dismissed rather than
 * disappearing into a toast.
 */
export default function ApiKeys() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<ApiKey>();
  const [copied, setCopied] = useState(false);
  const [revoking, setRevoking] = useState("");
  // Select is controlled, and mirrors its value into a hidden input for forms.
  const [permission, setPermission] = useState("read");
  const [days, setDays] = useState("90");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(() => {
    setLoading(true);
    api<ApiKey[]>("/me/api-keys")
      .then((x) => setKeys(x ?? []))
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);
  // Deferred past the render commit, as the dashboard does: calling setState
  // straight from an effect body kicks off a cascading render.
  useEffect(() => {
    const id = requestAnimationFrame(() => load());
    return () => cancelAnimationFrame(id);
  }, [load]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setBusy(true);
    setError("");
    try {
      const key = await api<ApiKey>("/me/api-keys", {
        method: "POST",
        body: JSON.stringify({
          name: String(form.get("name") ?? "").trim(),
          permission,
          ttl_seconds: Number(days) * 24 * 60 * 60,
        }),
      });
      setCreated(key);
      setCreating(false);
      setCopied(false);
      load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function revoke(key: ApiKey) {
    setRevoking(key.id);
    try {
      await api(`/me/api-keys/${key.id}`, { method: "DELETE" });
      setNotice(`Revoked ${key.name}. Anything signed in with it is signed out.`);
      load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRevoking("");
    }
  }

  async function copy(secret: string) {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      setError("Couldn't copy. Select the key and copy it manually.");
    }
  }

  return (
    <>
      <p>
        An API key signs you in without a browser: on a server, in a script, or through the{" "}
        <a href="https://docs.envisecrets.com/sdk">SDK</a>. It can do anything you can, up to the
        permission you give it. For something tied to one environment, create a service token instead.
      </p>

      {created && (
        <div className="key-reveal">
          <strong>{created.name} is ready</strong>
          <p>Copy it now. This is the only time it is shown, because only a hash of it is stored.</p>
          <div className="key-secret">
            <code>{created.secret}</code>
            <button type="button" className="button secondary" onClick={() => copy(created.secret ?? "")}>
              {copied ? <Check /> : <Clipboard />}
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <button type="button" className="button ghost" onClick={() => setCreated(undefined)}>
            Done
          </button>
        </div>
      )}

      {creating ? (
        <form className="key-form" onSubmit={create}>
          <label>
            Name
            <input name="name" required autoFocus placeholder="laptop, ci, staging-server" />
          </label>
          <div className="field">
            <span>Permission</span>
            <Select name="permission" ariaLabel="Permission" value={permission} onChange={setPermission} options={PERMISSIONS} />
          </div>
          <div className="field">
            <span>Expires</span>
            <Select name="days" ariaLabel="Expires" value={days} onChange={setDays} options={EXPIRY} />
          </div>
          <div className="key-form-actions">
            <button className="button primary" disabled={busy}>
              {busy ? "Creating..." : "Create key"}
            </button>
            <button type="button" className="button ghost" onClick={() => setCreating(false)} disabled={busy}>
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <button type="button" className="button secondary" onClick={() => setCreating(true)}>
          <Plus />
          New API key
        </button>
      )}

      {loading ? (
        <Loader label="Loading API keys" size={36} />
      ) : keys.length === 0 ? (
        <p className="key-empty">
          <KeyRound /> No API keys yet.
        </p>
      ) : (
        <ul className="key-list">
          {keys.map((key) => (
            <li key={key.id} className={expired(key) ? "expired" : undefined}>
              <span className="key-name">
                <strong>{key.name}</strong>
                <small>
                  {key.permission} · {describeExpiry(key)} · {describeLastUsed(key)}
                </small>
              </span>
              <button
                type="button"
                className="button ghost danger-text"
                onClick={() => revoke(key)}
                disabled={revoking === key.id}
                aria-label={`Revoke ${key.name}`}
              >
                <Trash2 />
                {revoking === key.id ? "Revoking..." : "Revoke"}
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="toast-stack">
        {error && <Toast key={"e" + error} kind="error" message={error} onClose={() => setError("")} />}
        {notice && <Toast key={"n" + notice} kind="notice" message={notice} onClose={() => setNotice("")} />}
      </div>
    </>
  );
}

function expired(key: ApiKey) {
  return key.expires_at !== null && new Date(key.expires_at) < new Date();
}

function describeExpiry(key: ApiKey) {
  if (key.expires_at === null) return "never expires";
  const when = new Date(key.expires_at);
  if (when < new Date()) return "expired";
  return `expires ${when.toLocaleDateString()}`;
}

function describeLastUsed(key: ApiKey) {
  return key.last_used_at ? `last used ${new Date(key.last_used_at).toLocaleDateString()}` : "never used";
}
