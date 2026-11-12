"use client";
import { FormEvent, useCallback, useEffect, useState } from "react";
import { Check, Clipboard, KeyRound, Plus, RefreshCw, Trash2, UserPlus, X } from "lucide-react";
import Select from "@/components/select";
import Toast from "@/components/toast";
import { api } from "@/lib/api";

/*
  The people who can sign in to this self-hosted instance, and the projects each one has.
  Nothing is emailed: a new or reset password is shown here once, for the admin to pass on.
*/

type Grant = { id: string; project_id: string; project_name: string; environment_id: string; environment_name: string; permission: string };
type Member = { id: string; email: string; role: "admin" | "contributor"; grants: Grant[] };
type Project = { ID: string; Name: string };
type Environment = { ID: string; Name: string; Production: boolean };
type Revealed = { email: string; password: string; isNew: boolean };

const PERMISSIONS = [
  { value: "read", label: "Read: view and pull" },
  { value: "write", label: "Write: push and change" },
  { value: "manage", label: "Manage: also manage access" },
];

export default function UsersPage() {
  const [members, setMembers] = useState<Member[]>();
  const [projects, setProjects] = useState<Project[]>([]);
  const [adding, setAdding] = useState(false);
  const [granting, setGranting] = useState<Member>();
  const [revealed, setRevealed] = useState<Revealed>();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(async () => {
    try {
      const [users, list] = await Promise.all([api<Member[]>("/instance/users"), api<Project[]>("/projects")]);
      setMembers(users);
      setProjects(list);
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  useEffect(() => {
    const id = requestAnimationFrame(() => void load());
    return () => cancelAnimationFrame(id);
  }, [load]);

  async function resetPassword(m: Member) {
    if (!confirm(`Give ${m.email} a new password? Their current one stops working.`)) return;
    try {
      const reply = await api<{ email: string; password: string }>(`/instance/users/${m.id}/password`, { method: "POST" });
      setRevealed({ email: reply.email, password: reply.password, isNew: false });
    } catch (e) { setError((e as Error).message); }
  }
  async function remove(m: Member) {
    if (!confirm(`Remove ${m.email}? They are signed out everywhere and lose every project.`)) return;
    try {
      await api(`/instance/users/${m.id}`, { method: "DELETE" });
      setNotice(`Removed ${m.email}.`);
      await load();
    } catch (e) { setError((e as Error).message); }
  }
  async function revoke(m: Member, g: Grant) {
    try {
      await api(`/projects/${g.project_id}/collaborators/${g.id}`, { method: "DELETE" });
      setNotice(`${m.email} no longer has ${g.project_name}${g.environment_name ? ` / ${g.environment_name}` : ""}.`);
      await load();
    } catch (e) { setError((e as Error).message); }
  }

  return <main className="product-page">
    <div className="page-title">
      <div>
        <span className="eyebrow">Instance</span>
        <h1>Users</h1>
        <p>Everyone who can sign in to this instance. Nothing is emailed: you give each person their password.</p>
      </div>
      <button className="button primary" onClick={() => setAdding(true)}><UserPlus />Add person</button>
    </div>

    <section className="plain-section">
      {!members ? <div className="skeleton-list" role="status"><span className="sr-only">Loading users</span>{[0, 1, 2].map((i) => <div className="skeleton-row" key={i}><span className="skeleton" /><span className="skeleton" /></div>)}</div>
        : <div className="data-table users-table">
          <div className="thead"><span>Person</span><span>Projects</span><span /></div>
          {members.map((m) => <div className="trow" key={m.email}>
            <span className="user-cell">{m.email}<span className={`badge ${m.role === "admin" ? "success" : ""}`}>{m.role === "admin" ? "Admin" : "Contributor"}</span></span>
            <span className="grant-list">
              {m.role === "admin" ? <span className="muted">Every project</span>
                : m.grants.length === 0 ? <span className="muted">No projects yet</span>
                : m.grants.map((g) => <span className="grant-chip" key={g.id}>
                  {g.project_name}{g.environment_name ? ` / ${g.environment_name}` : ""} · {g.permission}
                  <button title="Take away" onClick={() => revoke(m, g)}><X /></button>
                </span>)}
            </span>
            <div className="cell-actions">
              {m.role !== "admin" && <button className="icon-btn" title="Give a project" onClick={() => setGranting(m)}><Plus /></button>}
              <button className="icon-btn" title="New password" onClick={() => resetPassword(m)}><RefreshCw /></button>
              {m.role !== "admin" && <button className="icon-btn danger" title="Remove" onClick={() => remove(m)}><Trash2 /></button>}
            </div>
          </div>)}
        </div>}
    </section>

    {adding && <AddPerson close={() => setAdding(false)} onAdded={async (reply) => { setAdding(false); setRevealed({ ...reply, isNew: true }); await load(); }} />}
    {granting && <GiveAccess member={granting} projects={projects} close={() => setGranting(undefined)}
      onGranted={async (g) => { setGranting(undefined); setNotice(`${granting.email} now has ${g.project_name}.`); await load(); }} />}
    {revealed && <PasswordReveal {...revealed} close={() => setRevealed(undefined)} />}
    {error && <Toast kind="error" message={error} onClose={() => setError("")} />}
    {notice && <Toast kind="notice" message={notice} onClose={() => setNotice("")} />}
  </main>;
}

function Dialog({ title, sub, close, busy, error, submitLabel, onSubmit, children }: {
  title: string; sub: string; close: () => void; busy: boolean; error: string; submitLabel: string;
  onSubmit: () => Promise<void>; children: React.ReactNode;
}) {
  return <div className="dialog-backdrop" onMouseDown={busy ? undefined : close}>
    <form className="dialog" onMouseDown={(e) => e.stopPropagation()} onSubmit={(e: FormEvent) => { e.preventDefault(); void onSubmit(); }}>
      <header><h2>{title}</h2><button type="button" onClick={close} disabled={busy}>×</button></header>
      <p className="dialog-sub">{sub}</p>
      {children}
      {error && <p className="form-error">{error}</p>}
      <button className="button primary" disabled={busy}>{busy && <span className="spinner" />}{busy ? "Saving..." : submitLabel}</button>
    </form>
  </div>;
}

function AddPerson({ close, onAdded }: { close: () => void; onAdded: (reply: { email: string; password: string }) => Promise<void> }) {
  const [email, setEmail] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return <Dialog title="Add person" sub="They get a generated password, shown to you once. Give them projects afterwards."
    close={close} busy={busy} error={error} submitLabel="Add and show password"
    onSubmit={async () => {
      setBusy(true); setError("");
      try { await onAdded(await api("/instance/users", { method: "POST", body: JSON.stringify({ email }) })); }
      catch (e) { setBusy(false); setError((e as Error).message); }
    }}>
    <label>Email<input autoFocus required type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="teammate@company.com" /></label>
  </Dialog>;
}

function GiveAccess({ member, projects, close, onGranted }: {
  member: Member; projects: Project[]; close: () => void; onGranted: (g: Grant) => Promise<void>;
}) {
  const [projectId, setProjectId] = useState(projects[0]?.ID ?? "");
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [envId, setEnvId] = useState("");
  const [permission, setPermission] = useState("read");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!projectId) return;
    let live = true;
    api<Environment[]>(`/projects/${projectId}/environments`).then((list) => { if (live) { setEnvironments(list); setEnvId(""); } }).catch(() => {});
    return () => { live = false; };
  }, [projectId]);

  if (!projects.length) return <Dialog title="Give a project" sub="Create a project first, then give it to people here."
    close={close} busy={false} error="" submitLabel="Close" onSubmit={async () => close()}>{null}</Dialog>;

  return <Dialog title={`Give ${member.email} a project`} sub="All non-production environments, or one environment, production included."
    close={close} busy={busy} error={error} submitLabel="Give access"
    onSubmit={async () => {
      setBusy(true); setError("");
      try {
        const grant = await api<Grant>(`/projects/${projectId}/collaborators`, { method: "POST", body: JSON.stringify({ user_id: member.id, permission, environment_id: envId }) });
        await onGranted(grant);
      } catch (e) { setBusy(false); setError((e as Error).message); }
    }}>
    <div className="field"><span>Project</span><Select ariaLabel="Project" value={projectId} onChange={setProjectId} options={projects.map((p) => ({ value: p.ID, label: p.Name }))} /></div>
    <div className="field"><span>Environment</span><Select ariaLabel="Environment" value={envId} onChange={setEnvId}
      options={[{ value: "", label: "All non-production" }, ...environments.map((e) => ({ value: e.ID, label: e.Production ? `${e.Name} (production)` : e.Name }))]} /></div>
    <div className="field"><span>Permission</span><Select ariaLabel="Permission" value={permission} onChange={setPermission} options={PERMISSIONS} /></div>
  </Dialog>;
}

function PasswordReveal({ email, password, isNew, close }: Revealed & { close: () => void }) {
  const [copied, setCopied] = useState(false);
  return <div className="dialog-backdrop">
    <div className="dialog" role="dialog" aria-modal="true">
      <header><h2>{isNew ? `${email} can sign in` : `New password for ${email}`}</h2></header>
      <p className="dialog-sub">Give them this password yourself. It is shown only now; if it&rsquo;s lost, reset it again.</p>
      <div className="password-reveal">
        <KeyRound />
        <code>{password}</code>
        <button type="button" className="icon-btn" title="Copy" onClick={async () => { await navigator.clipboard.writeText(password); setCopied(true); }}>{copied ? <Check /> : <Clipboard />}</button>
      </div>
      <p className="dialog-sub">They sign in at <strong>{typeof window === "undefined" ? "" : window.location.origin}/auth</strong> and can change it in Settings. From the CLI: <code>envi set-url</code> with this instance&rsquo;s API URL, then <code>envi auth</code>.</p>
      <button className="button primary" onClick={close}>Done</button>
    </div>
  </div>;
}
