"use client";
import { Activity, Check, Clipboard, Clock, Eye, EyeOff, Folder, FolderPlus, KeyRound, Pencil, Plus, RefreshCw, Search, Shield, Trash2, UploadCloud, UserPlus } from "lucide-react";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import Select from "@/components/select";
import { filterKeys, parseDotenv, relativeTime } from "../utils";

type Project = { ID: string; OrgID: string; Name: string };
type Env = { ID: string; Name: string; Production: boolean };
type Snap = { values: Record<string, string>; revision: number };
type Event = { action: string; target_type: string; target_id: string; actor: string; created_at: string };
type Collaborator = { id: string; email: string; permission: string; environment_id?: string; status: "active" | "pending"; expires_at?: string };
type Page = "overview" | "secrets" | "projects" | "sharing" | "activity";

const PERM_ICON: Record<string, React.ReactNode> = { read: <Eye />, write: <Pencil />, manage: <Shield /> };

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
  const [collaborators, setCollaborators] = useState<Collaborator[]>([]);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [query, setQuery] = useState("");
  const [reveal, setReveal] = useState(new Set<string>());
  const [copied, setCopied] = useState("");
  const [dragging, setDragging] = useState(false);
  const [modal, setModal] = useState<"project" | "secret" | "share" | "import">();
  // Every list starts in flight rather than empty. Without this, the first
  // paint renders "No projects yet" against state that simply hasn't been
  // fetched, which reads as a bug rather than as loading.
  const [loadingProjects, setLoadingProjects] = useState(true);
  const [loadingEnv, setLoadingEnv] = useState(true);
  const [loadingSecrets, setLoadingSecrets] = useState(true);
  const [loadingEvents, setLoadingEvents] = useState(true);
  const [loadingCollaborators, setLoadingCollaborators] = useState(true);

  const loadProjects = useCallback(() => {
    setLoadingProjects(true);
    return api<Project[]>("/projects").then((x) => {
      const list = x ?? [];
      setProjects(list);
      setProject((p) => list.find((i) => i.ID === p?.ID) || list[0]);
      // Nothing to select means the env/secret fetches below never run, so
      // release their spinners here or they would hang forever.
      if (!list.length) { setLoadingEnv(false); setLoadingSecrets(false); }
    }).catch((e) => { setError(e.message); setLoadingEnv(false); setLoadingSecrets(false); })
      .finally(() => setLoadingProjects(false));
  }, []);
  useEffect(() => {
    const id = requestAnimationFrame(() => void loadProjects());
    return () => cancelAnimationFrame(id);
  }, [loadProjects]);

  // Resolve the project's single environment, provisioning one on the fly if the
  // project has none. New envs are non-production so access is never denied.
  const loadEnv = useCallback(() => {
    if (!project) { setEnv(undefined); setLoadingEnv(false); return Promise.resolve(); }
    setLoadingEnv(true);
    const pid = project.ID;
    return api<Env[]>(`/projects/${pid}/environments`).then(async (x) => {
      let list = x ?? [];
      if (!list.length) {
        const created = await api<Env>(`/projects/${pid}/environments`, { method: "POST", body: JSON.stringify({ name: "default", is_production: false }) });
        list = [created];
      }
      setEnv((e) => list.find((i) => i.ID === e?.ID) || list[0]);
    }).catch((e) => setError(e.message)).finally(() => setLoadingEnv(false));
  }, [project]);
  useEffect(() => {
    const id = requestAnimationFrame(() => void loadEnv());
    return () => cancelAnimationFrame(id);
  }, [loadEnv]);

  const loadSecrets = useCallback(() => {
    if (!env) { setSnap({ values: {}, revision: 0 }); setLoadingSecrets(false); return; }
    setLoadingSecrets(true);
    api<Snap>(`/environments/${env.ID}/secrets/snapshot`).then((x) => setSnap({ values: x?.values ?? {}, revision: x?.revision ?? 0 }))
      .catch((e) => setError(e.message)).finally(() => setLoadingSecrets(false));
  }, [env]);
  useEffect(() => {
    const id = requestAnimationFrame(() => loadSecrets());
    return () => cancelAnimationFrame(id);
  }, [loadSecrets]);

  const loadEvents = useCallback(() => {
    if (!project) { setEvents([]); setLoadingEvents(false); return; }
    setLoadingEvents(true);
    api<Event[]>(`/orgs/${project.OrgID}/audit-events`).then((x) => setEvents(x ?? []))
      .catch((e) => setError(e.message)).finally(() => setLoadingEvents(false));
  }, [project]);
  useEffect(() => {
    if (page !== "activity") return;
    const id = requestAnimationFrame(() => loadEvents());
    return () => cancelAnimationFrame(id);
  }, [page, loadEvents]);

  const loadCollaborators = useCallback(() => {
    if (!project) { setCollaborators([]); setLoadingCollaborators(false); return; }
    setLoadingCollaborators(true);
    api<Collaborator[]>(`/projects/${project.ID}/collaborators`).then((x) => setCollaborators(x ?? []))
      .catch((e) => setError(e.message)).finally(() => setLoadingCollaborators(false));
  }, [project]);
  useEffect(() => {
    if (page !== "sharing") return;
    // Deferred a frame: these loaders can reset state synchronously (no
    // project selected), and that needs to land outside the effect's own
    // synchronous pass.
    const id = requestAnimationFrame(() => loadCollaborators());
    return () => cancelAnimationFrame(id);
  }, [page, loadCollaborators]);

  // Project and environment resolve as a waterfall, so anything downstream of
  // them is still loading while either is in flight.
  const contextLoading = loadingProjects || loadingEnv;

  async function revokeCollaborator(c: Collaborator) {
    if (!project || !confirm(c.status === "pending" ? `Cancel the invitation to ${c.email}?` : `Remove ${c.email}'s access?`)) return;
    try {
      const path = c.status === "pending" ? `/projects/${project.ID}/invitations/${c.id}` : `/projects/${project.ID}/collaborators/${c.id}`;
      await api(path, { method: "DELETE" });
      setCollaborators((prev) => prev.filter((x) => x.id !== c.id));
      setNotice(c.status === "pending" ? `Cancelled the invitation to ${c.email}.` : `Removed ${c.email}'s access.`);
    } catch (e) { setError((e as Error).message); }
  }

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
      await api<{ Token: string }>(`/projects/${project.ID}/invitations`, { method: "POST", body: JSON.stringify({ email: data.email, environment_id: env.ID, permission: data.permission }) });
      setNotice(`Invitation sent to ${data.email}. They can accept it even without an existing account.`);
      loadCollaborators();
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
      {/* a plain div, not a <label>: clicking a label forwards the click to
          the control inside it, which would immediately re-toggle the menu */}
      <div className="field field-grow">
        <span>Project</span>
        <Select
          ariaLabel="Project"
          placeholder={projects.length ? "Select a project" : "No projects yet"}
          value={project?.ID || ""}
          options={projects.map((p) => ({ value: p.ID, label: p.Name }))}
          onChange={(id) => setProject(projects.find((p) => p.ID === id))}
        />
      </div>
    </div>}

    {error && <div className="alert error"><button className="alert-dismiss" onClick={() => setError("")}>×</button>{error}</div>}
    {notice && <div className="alert notice"><button className="alert-dismiss" onClick={() => setNotice("")}>×</button>{notice}</div>}

    {page === "overview" && <>
      <div className="stat-cards">
        <div className="stat-card"><div className="stat-icon"><Folder /></div><span>Projects</span>{loadingProjects ? <span className="spinner" /> : <strong>{projects.length}</strong>}</div>
        <div className="stat-card"><div className="stat-icon"><KeyRound /></div><span>Secrets in {project?.Name || "project"}</span>{contextLoading || loadingSecrets ? <span className="spinner" /> : <strong>{Object.keys(snap.values).length}</strong>}</div>
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
      {contextLoading || loadingSecrets ? <Loading label="Loading secrets" /> : keys.length ? <div className="data-table">
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

    {page === "projects" && (loadingProjects ? <Loading label="Loading projects" /> : <div className="project-grid">
      {projects.map((p) => <button key={p.ID} className={"project-card" + (p.ID === project?.ID ? " active" : "")} onClick={() => setProject(p)}>
        <div className="proj-top"><div className="proj-icon"><Folder /></div>{p.ID === project?.ID && <span className="badge selected">Selected</span>}</div>
        <strong>{p.Name}</strong><small>{p.ID.slice(0, 8)}</small>
      </button>)}
      <button className="project-card new" onClick={() => setModal("project")}><FolderPlus /><strong>New project</strong></button>
    </div>)}

    {page === "sharing" && <section className="plain-section">
      <h2>Invite collaborators to a project</h2>
      <p>Pick a project above, then grant a collaborator scoped permission to its secrets. They&rsquo;ll get an email with a link to accept — signing up first if they don&rsquo;t have an account yet.</p>
      <ul className="perm-list">
        <li><Eye />Read — view and pull secrets</li>
        <li><Pencil />Write — push and change secrets</li>
        <li><Shield />Manage — invite others and manage access</li>
      </ul>
      {contextLoading || loadingCollaborators ? <Loading label="Loading collaborators" /> : collaborators.length ? <div className="data-table">
        <div className="thead"><span>Collaborator</span><span>Access</span><span /></div>
        {collaborators.map((c) => <div className="trow" key={c.id}>
          <span>{c.email}</span>
          <span className="perm-cell">
            {PERM_ICON[c.permission]}{c.permission}
            {c.status === "pending"
              ? <span className="badge pending"><Clock />Pending{c.expires_at ? ` · expires ${new Date(c.expires_at).toLocaleDateString()}` : ""}</span>
              : <span className="badge success">Active</span>}
          </span>
          <div className="cell-actions">
            <button className="icon-btn danger" title={c.status === "pending" ? "Cancel invitation" : "Remove access"} onClick={() => revokeCollaborator(c)}><Trash2 /></button>
          </div>
        </div>)}
      </div> : <Empty icon={<UserPlus />} title={project ? "No collaborators yet" : "No project selected"}
        text={project ? "Invite someone above — they'll show up here as pending until they accept." : "Choose a project above to see who has access."} />}
    </section>}

    {page === "activity" && <section className="panel">
      {contextLoading || loadingEvents ? <Loading label="Loading activity" /> : events.length ? <div className="timeline">
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

function Loading({ label }: { label: string }) {
  return <div className="loading-state" role="status" aria-live="polite"><span className="spinner" /><p>{label}</p></div>;
}

const DIALOG_META: Record<string, { title: string; sub?: string }> = {
  project: { title: "New project" },
  secret: { title: "Add secret" },
  share: { title: "Invite collaborator", sub: "They'll get an email with a link to accept — signing up first if they don't have an account yet." },
};
function Dialog({ type, close, submit }: { type: "project" | "secret" | "share"; close: () => void; submit: (d: Record<string, string>) => Promise<void> }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // Controlled so the glass Select can mirror it into a hidden input, which
  // is what keeps this form readable through FormData.
  const [permission, setPermission] = useState("read");
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
      {type === "share" && <><Field name="email" label="Email" type="email" placeholder="teammate@company.com" /><div className="field"><span>Permission</span><Select name="permission" ariaLabel="Permission" value={permission} onChange={setPermission} options={[{ value: "read", label: "read" }, { value: "write", label: "write" }, { value: "manage", label: "manage" }]} /></div></>}
      {error && <p className="form-error">{error}</p>}
      <button className="button primary" disabled={busy}>{busy && <span className="spinner" />}{busy ? "Saving..." : "Save"}</button>
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
  async function readDroppedFile(file?: File | null) { if (!file) return; setFileName(file.name); setText(await file.text()); }
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
        onDrop={(e) => { e.preventDefault(); setDragging(false); void readDroppedFile(e.dataTransfer.files?.[0]); }}>
        <UploadCloud />
        <strong>{fileName || "Drop a .env file or click to choose"}</strong>
        <small>KEY=VALUE lines · comments and quotes supported</small>
        <input type="file" accept=".env,text/plain" hidden onChange={(e) => void readDroppedFile(e.target.files?.[0])} />
      </label>
      <label>Or paste contents<textarea value={text} onChange={(e) => setText(e.target.value)} placeholder={"API_KEY=sk_live_...\nDATABASE_URL=postgres://..."} /></label>
      {error && <p className="form-error">{error}</p>}
      <button className="button primary" disabled={busy}>{busy && <span className="spinner" />}{busy ? "Importing..." : count ? `Import ${count} secret${count > 1 ? "s" : ""}` : "Import"}</button>
    </form>
  </div>;
}
