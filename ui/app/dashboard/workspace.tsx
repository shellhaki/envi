"use client";
import { Activity, Check, Clipboard, Eye, EyeOff, Folder, FolderPlus, KeyRound, Pencil, Plus, RefreshCw, Search, Shield, Trash2, UploadCloud, UserPlus } from "lucide-react";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { filterKeys, parseDotenv, relativeTime } from "../utils";

type Project = { ID: string; OrgID: string; Name: string };
type Env = { ID: string; Name: string; Production: boolean };
type Snap = { values: Record<string, string>; revision: number };
type Event = { action: string; target_type: string; target_id: string; actor: string; created_at: string };
type Page = "overview" | "secrets" | "projects" | "sharing" | "activity";

async function api<T>(path: string, init: RequestInit = {}) {
  const r = await fetch("/api/envi" + path, { ...init, headers: { "Content-Type": "application/json", ...init.headers } });
  const b = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(b.error || "Request failed");
  return b as T;
}

const TITLES: Record<Page, string> = { overview: "Overview", secrets: "Secrets", projects: "Projects", sharing: "Sharing", activity: "Activity" };
const SUBTITLES: Record<Page, string> = {
  overview: "Your workspace at a glance.",
  secrets: "Encrypted values for this project.",
  projects: "Your projects.",
  sharing: "Grant collaborators scoped access.",
  activity: "Recent reads and changes across your org.",
};
function actionLabel(a: string) {
  const m: Record<string, string> = { "secret.read": "Read secrets", "secret.write": "Updated secrets", "secret.delete": "Deleted a secret" };
  return m[a] || a.replace(/[._]/g, " ").replace(/^\w/, (c) => c.toUpperCase());
}

export default function Workspace({ page }: { page: Page }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [project, setProject] = useState<Project>();
  // Each project transparently uses a single environment. It is never surfaced
  // in the UI — secrets, imports, and sharing all target this one env.
  const [env, setEnv] = useState<Env>();
  const [snap, setSnap] = useState<Snap>({ values: {}, revision: 0 });
  const [events, setEvents] = useState<Event[]>([]);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [reveal, setReveal] = useState(new Set<string>());
  const [copied, setCopied] = useState("");
  const [dragging, setDragging] = useState(false);
  const [modal, setModal] = useState<"project" | "secret" | "share" | "import">();

  const loadProjects = useCallback(() => api<Project[]>("/projects").then((x) => {
    const list = x ?? [];
    setProjects(list);
    setProject((p) => list.find((i) => i.ID === p?.ID) || list[0]);
  }).catch((e) => setError(e.message)), []);
  useEffect(() => { void loadProjects(); }, [loadProjects]);

  // Resolve the project's single environment, provisioning one on the fly if the
  // project has none. New envs are non-production so access is never denied.
  const loadEnv = useCallback(() => {
    if (!project) { setEnv(undefined); return Promise.resolve(); }
    const pid = project.ID;
    return api<Env[]>(`/projects/${pid}/environments`).then(async (x) => {
      let list = x ?? [];
      if (!list.length) {
        const created = await api<Env>(`/projects/${pid}/environments`, { method: "POST", body: JSON.stringify({ name: "default", is_production: false }) });
        list = [created];
      }
      setEnv((e) => list.find((i) => i.ID === e?.ID) || list[0]);
    }).catch((e) => setError(e.message));
  }, [project]);
  useEffect(() => { void loadEnv(); }, [loadEnv]);

  const loadSecrets = useCallback(() => {
    if (!env) { setSnap({ values: {}, revision: 0 }); return; }
    api<Snap>(`/environments/${env.ID}/secrets/snapshot`).then((x) => setSnap({ values: x?.values ?? {}, revision: x?.revision ?? 0 })).catch((e) => setError(e.message));
  }, [env]);
  useEffect(() => { void loadSecrets(); }, [loadSecrets]);

  const loadEvents = useCallback(() => {
    if (project) api<Event[]>(`/orgs/${project.OrgID}/audit-events`).then((x) => setEvents(x ?? [])).catch((e) => setError(e.message));
  }, [project]);
  useEffect(() => { if (page === "activity") loadEvents(); }, [page, loadEvents]);

  const keys = useMemo(() => filterKeys(snap.values, query), [snap, query]);

  async function submit(type: string, data: Record<string, string>) {
    if (type === "project") {
      const me = await api<{ OrganizationID: string }>("/me");
      const created = await api<Project>("/projects", { method: "POST", body: JSON.stringify({ org_id: me.OrganizationID, name: data.name }) });
      await loadProjects();
      if (created?.ID) setProject(created);
      setNotice(`Created project ${data.name}.`);
    }
    if (type === "secret" && env) {
      const values = { ...snap.values, [data.key]: data.value ?? "" };
      const x = await api<{ revision: number }>(`/environments/${env.ID}/secrets/snapshot`, { method: "PUT", body: JSON.stringify({ values, expected_revision: snap.revision }) });
      setSnap({ values, revision: x.revision });
      setNotice(`Saved ${data.key}.`);
    }
    if (type === "share" && project && env) {
      const x = await api<{ Token: string }>(`/projects/${project.ID}/invitations`, { method: "POST", body: JSON.stringify({ email: data.email, environment_id: env.ID, permission: data.permission }) });
      setNotice(`Invite token for ${data.email}: ${x.Token}`);
    }
    setModal(undefined);
  }

  // importValues merges parsed KEY=VALUE pairs into the current project.
  // Throws on failure so both the drop target and the import dialog can react.
  async function importValues(incoming: Record<string, string>) {
    if (!env) throw new Error("Select a project first.");
    const count = Object.keys(incoming).length;
    if (!count) throw new Error("No secrets found in that file.");
    const values = { ...snap.values, ...incoming };
    const x = await api<{ revision: number }>(`/environments/${env.ID}/secrets/snapshot`, { method: "PUT", body: JSON.stringify({ values, expected_revision: snap.revision }) });
    setSnap({ values, revision: x.revision });
    setNotice(`Imported ${count} secret${count > 1 ? "s" : ""} into ${project?.Name ?? "project"}.`);
  }
  async function onDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragging(false);
    const file = e.dataTransfer.files?.[0];
    if (!file) return;
    try { await importValues(parseDotenv(await file.text())); } catch (x) { setError((x as Error).message); }
  }

  function toggleReveal(k: string) { setReveal((prev) => { const n = new Set(prev); if (n.has(k)) n.delete(k); else n.add(k); return n; }); }
  function copy(k: string) { navigator.clipboard.writeText(snap.values[k]); setCopied(k); setTimeout(() => setCopied((c) => (c === k ? "" : c)), 1200); }
  async function removeKey(k: string) {
    if (!env || !confirm(`Delete ${k}?`)) return;
    try { await api(`/environments/${env.ID}/secrets/${encodeURIComponent(k)}`, { method: "DELETE" }); loadSecrets(); setNotice(`Deleted ${k}.`); } catch (e) { setError((e as Error).message); }
  }

  const showContext = page !== "projects";
  const titleAction = page === "projects" ? <button className="button primary" onClick={() => setModal("project")}><Plus />New project</button>
    : page === "sharing" ? <button className="button primary" onClick={() => setModal("share")} disabled={!project}><UserPlus />Invite</button>
    : page === "activity" ? <button className="button secondary" onClick={() => loadEvents()}><RefreshCw />Refresh</button>
    : null;

  return <main className="product-page">
    <div className="page-title">
      <div><span className="eyebrow">Workspace</span><h1>{TITLES[page]}</h1><p>{SUBTITLES[page]}</p></div>
      {titleAction}
    </div>

    {showContext && <div className="context-bar">
      <label className="field field-grow">
        <span>Project</span>
        <select value={project?.ID || ""} onChange={(e) => setProject(projects.find((p) => p.ID === e.target.value))}>
          {!projects.length && <option value="">No projects yet</option>}
          {projects.map((p) => <option key={p.ID} value={p.ID}>{p.Name}</option>)}
        </select>
      </label>
    </div>}

    {error && <div className="alert error"><button className="alert-dismiss" onClick={() => setError("")}>×</button>{error}</div>}
    {notice && <div className="alert notice"><button className="alert-dismiss" onClick={() => setNotice("")}>×</button><code>{notice}</code></div>}

    {page === "overview" && <>
      <div className="stat-cards">
        <div className="stat-card"><div className="stat-icon"><Folder /></div><span>Projects</span><strong>{projects.length}</strong></div>
        <div className="stat-card"><div className="stat-icon"><KeyRound /></div><span>Secrets in {project?.Name || "project"}</span><strong>{Object.keys(snap.values).length}</strong></div>
      </div>
      <div className="quick-actions">
        <button className="button primary" onClick={() => setModal("project")}><Plus />New project</button>
        <button className="button secondary" onClick={() => setModal("secret")} disabled={!project}><Plus />Add secret</button>
        <button className="button secondary" onClick={() => setModal("import")} disabled={!project}><UploadCloud />Import .env</button>
        <button className="button secondary" onClick={() => setModal("share")} disabled={!project}><UserPlus />Invite collaborator</button>
      </div>
    </>}

    {page === "secrets" && <section className={"panel drop-target" + (dragging ? " dragging" : "")}
      onDragOver={(e) => { if (project) { e.preventDefault(); setDragging(true); } }}
      onDragLeave={() => setDragging(false)} onDrop={onDrop}>
      <div className="panel-head">
        <div className="panel-search"><Search /><input placeholder="Filter keys" value={query} onChange={(e) => setQuery(e.target.value)} /></div>
        <div className="panel-tools">
          <span className="badge">rev {snap.revision}</span>
          <button className="button ghost" onClick={() => loadSecrets()}><RefreshCw />Refresh</button>
          <button className="button secondary" onClick={() => setModal("import")} disabled={!project}><UploadCloud />Import .env</button>
          <button className="button primary" onClick={() => setModal("secret")} disabled={!project}><Plus />Add secret</button>
        </div>
      </div>
      {dragging && <div className="drop-hint"><UploadCloud />Drop a .env file to import its keys</div>}
      {keys.length ? <div className="data-table">
        <div className="thead"><span>Key</span><span>Value</span><span /></div>
        {keys.map((k) => <div className="trow" key={k}>
          <code>{k}</code>
          <code className="val">{reveal.has(k) ? snap.values[k] : "•".repeat(12)}</code>
          <div className="cell-actions">
            <button className="icon-btn" title={reveal.has(k) ? "Hide" : "Reveal"} onClick={() => toggleReveal(k)}>{reveal.has(k) ? <EyeOff /> : <Eye />}</button>
            <button className="icon-btn" title="Copy" onClick={() => copy(k)}>{copied === k ? <Check /> : <Clipboard />}</button>
            <button className="icon-btn danger" title="Delete" onClick={() => removeKey(k)}><Trash2 /></button>
          </div>
        </div>)}
      </div> : <Empty icon={<KeyRound />} title={project ? "No secrets yet" : "No project selected"}
        text={project ? "Add a secret, or drag a .env file anywhere on this panel to import it." : "Choose a project above to view its secrets."} />}
    </section>}

    {page === "projects" && <div className="project-grid">
      {projects.map((p) => <button key={p.ID} className={"project-card" + (p.ID === project?.ID ? " active" : "")} onClick={() => setProject(p)}>
        <div className="proj-top"><div className="proj-icon"><Folder /></div>{p.ID === project?.ID && <span className="badge success">Selected</span>}</div>
        <strong>{p.Name}</strong><small>{p.ID.slice(0, 8)}</small>
      </button>)}
      <button className="project-card new" onClick={() => setModal("project")}><FolderPlus /><strong>New project</strong></button>
    </div>}

    {page === "sharing" && <section className="plain-section">
      <h2>Invite collaborators to a project</h2>
      <p>Pick a project above, then grant a collaborator scoped permission to its secrets.</p>
      <ul className="perm-list">
        <li><Eye />Read — view and pull secrets</li>
        <li><Pencil />Write — push and change secrets</li>
        <li><Shield />Manage — invite others and manage access</li>
      </ul>
      <button className="button primary" onClick={() => setModal("share")} disabled={!project}><UserPlus />Invite collaborator</button>
    </section>}

    {page === "activity" && <section className="panel">
      {events.length ? <div className="timeline">
        {events.map((x, i) => {
          const kind = x.action.includes("delete") ? "delete" : x.action.includes("write") ? "write" : "read";
          const Icon = kind === "delete" ? Trash2 : kind === "write" ? Pencil : Eye;
          return <div className="activity-item" key={i}>
            <div className={"activity-icon " + kind}><Icon /></div>
            <div className="activity-body">
              <strong>{actionLabel(x.action)}</strong>
              <small>{x.actor || "Service token"} · {x.target_type}{x.target_id && <> · <code>{x.target_id.slice(0, 8)}</code></>}</small>
            </div>
            <span className="activity-when">{relativeTime(x.created_at)}</span>
          </div>;
        })}
      </div> : <Empty icon={<Activity />} title="No activity yet" text="Reads, writes, and deletes on your secrets will show up here." />}
    </section>}

    {modal === "import" && <ImportDialog projectName={project?.Name || ""} close={() => setModal(undefined)} onImport={importValues} />}
    {modal && modal !== "import" && <Dialog type={modal} close={() => setModal(undefined)} submit={(d) => submit(modal, d)} />}
  </main>;
}

function Empty({ icon, title, text }: { icon: React.ReactNode; title: string; text: string }) {
  return <div className="empty"><div className="empty-icon">{icon}</div><strong>{title}</strong><p>{text}</p></div>;
}

const DIALOG_META: Record<string, { title: string; sub?: string }> = {
  project: { title: "New project" },
  secret: { title: "Add secret" },
  share: { title: "Invite collaborator", sub: "They receive a token to accept the invitation." },
};
function Dialog({ type, close, submit }: { type: "project" | "secret" | "share"; close: () => void; submit: (d: Record<string, string>) => Promise<void> }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const meta = DIALOG_META[type];
  return <div className="dialog-backdrop" onMouseDown={close}>
    <form className="dialog" onMouseDown={(e) => e.stopPropagation()} onSubmit={async (e: FormEvent<HTMLFormElement>) => {
      e.preventDefault(); setBusy(true); setError("");
      try { await submit(Object.fromEntries(new FormData(e.currentTarget).entries()) as Record<string, string>); }
      catch (x) { setBusy(false); setError((x as Error).message); }
    }}>
      <header><h2>{meta.title}</h2><button type="button" onClick={close}>×</button></header>
      {meta.sub && <p className="dialog-sub">{meta.sub}</p>}
      {type === "project" && <Field name="name" label="Project name" placeholder="acme-api" />}
      {type === "secret" && <><Field name="key" label="Key" placeholder="API_KEY" /><label>Value<textarea name="value" placeholder="secret value" /></label></>}
      {type === "share" && <><Field name="email" label="Email" type="email" placeholder="teammate@company.com" /><label>Permission<select name="permission" defaultValue="read"><option value="read">read</option><option value="write">write</option><option value="manage">manage</option></select></label></>}
      {error && <p className="form-error">{error}</p>}
      <button className="button primary" disabled={busy}>{busy ? "Saving..." : "Save"}</button>
    </form>
  </div>;
}
function Field({ name, label, type = "text", placeholder }: { name: string; label: string; type?: string; placeholder?: string }) {
  return <label>{label}<input required name={name} type={type} placeholder={placeholder} /></label>;
}

function ImportDialog({ projectName, close, onImport }: { projectName: string; close: () => void; onImport: (v: Record<string, string>) => Promise<void> }) {
  const [text, setText] = useState("");
  const [fileName, setFileName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dragging, setDragging] = useState(false);
  async function useFile(file?: File | null) { if (!file) return; setFileName(file.name); setText(await file.text()); }
  const parsed = parseDotenv(text);
  const count = Object.keys(parsed).length;
  return <div className="dialog-backdrop" onMouseDown={close}>
    <form className="dialog" onMouseDown={(e) => e.stopPropagation()} onSubmit={async (e) => {
      e.preventDefault();
      if (!count) { setError("No valid KEY=VALUE lines found."); return; }
      setBusy(true); setError("");
      try { await onImport(parsed); close(); } catch (x) { setBusy(false); setError((x as Error).message); }
    }}>
      <header><h2>Import .env</h2><button type="button" onClick={close}>×</button></header>
      <p className="dialog-sub">Parsed keys are merged into {projectName || "the selected project"}; existing keys with the same name are overwritten.</p>
      <label className={"dropzone" + (dragging ? " dragging" : "")}
        onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => { e.preventDefault(); setDragging(false); void useFile(e.dataTransfer.files?.[0]); }}>
        <UploadCloud />
        <strong>{fileName || "Drop a .env file or click to choose"}</strong>
        <small>KEY=VALUE lines · comments and quotes supported</small>
        <input type="file" accept=".env,text/plain" hidden onChange={(e) => void useFile(e.target.files?.[0])} />
      </label>
      <label>Or paste contents<textarea value={text} onChange={(e) => setText(e.target.value)} placeholder={"API_KEY=sk_live_...\nDATABASE_URL=postgres://..."} /></label>
      {error && <p className="form-error">{error}</p>}
      <button className="button primary" disabled={busy}>{busy ? "Importing..." : count ? `Import ${count} secret${count > 1 ? "s" : ""}` : "Import"}</button>
    </form>
  </div>;
}
