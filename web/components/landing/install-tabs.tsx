"use client";
import { useState } from "react";
import { Check, Clipboard, Container, Monitor, Server, Terminal, Workflow } from "lucide-react";

// Getting started, one tab per place Envi runs. Each tab is the real commands, copyable in one go.

type Tab = { id: string; label: string; icon: typeof Server; file: string; lines: string[] };

const TABS: Tab[] = [
  {
    id: "instance", label: "Your server", icon: Server, file: "ssh root@your-server",
    lines: [
      "# Installs the dashboard, the API, Postgres and Redis, with HTTPS",
      "curl -fsSL https://install.envisecrets.com | sh -s instance",
      "",
      "# Then, any time",
      "envi-instance status",
      "envi-instance backup",
      "envi-instance update",
    ],
  },
  {
    id: "cli", label: "macOS & Linux", icon: Terminal, file: "~/code/northwind-api",
    lines: [
      "curl -fsSL https://install.envisecrets.com | sh",
      "",
      "envi auth          # Envi Cloud or your own instance",
      "envi init          # link this folder to a project",
      "envi push          # encrypt and upload your .env",
      "envi run -- npm start",
    ],
  },
  {
    id: "windows", label: "Windows", icon: Monitor, file: "PowerShell",
    lines: [
      "irm https://install.envisecrets.com/install.ps1 | iex",
      "",
      "envi auth",
      "envi init",
      "envi run -- npm start",
    ],
  },
  {
    id: "docker", label: "Docker", icon: Container, file: "Dockerfile",
    lines: [
      "FROM node:22-slim",
      "RUN apt-get update && apt-get install -y curl",
      "RUN curl -fsSL https://install.envisecrets.com | ENVI_INSTALL_DIR=/usr/local/bin sh",
      "COPY . /app",
      "WORKDIR /app",
      "# ENVI_TOKEN is a service token, passed in at runtime",
      'CMD ["envi", "run", "--", "node", "server.js"]',
    ],
  },
  {
    id: "ci", label: "GitHub Actions", icon: Workflow, file: ".github/workflows/deploy.yml",
    lines: [
      "- name: Install envi",
      "  run: |",
      "    curl -fsSL https://install.envisecrets.com | sh",
      '    echo "$HOME/.local/bin" >> "$GITHUB_PATH"',
      "",
      "- name: Deploy",
      "  run: envi run -- ./deploy.sh",
      "  env:",
      "    ENVI_TOKEN: ${{ secrets.ENVI_TOKEN }}",
    ],
  },
];

export default function InstallTabs() {
  const [active, setActive] = useState(TABS[0].id);
  const [copied, setCopied] = useState(false);
  const tab = TABS.find((t) => t.id === active) ?? TABS[0];

  async function copy() {
    try {
      await navigator.clipboard.writeText(tab.lines.join("\n"));
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch { /* the code is on screen to select by hand */ }
  }

  return <div className="install-tabs">
    <div className="install-tab-row" role="tablist">
      {TABS.map(({ id, label, icon: Icon }) => <button key={id} role="tab" aria-selected={id === active} className={id === active ? "active" : undefined} onClick={() => setActive(id)}>
        <span className="install-tab-icon"><Icon /></span>{label}
      </button>)}
    </div>
    <div className="code-window">
      <div className="code-head">
        <span>{tab.file}</span>
        <button className="icon-button" onClick={copy} aria-label="Copy">{copied ? <Check /> : <Clipboard />}</button>
      </div>
      <pre key={tab.id}>{tab.lines.map((line, i) => <div key={i} className="code-line">
        <span className="code-num">{i + 1}</span>
        <code>{highlight(line)}</code>
      </div>)}</pre>
    </div>
  </div>;
}

// highlight colours comments, strings and the envi commands; everything else stays plain.
function highlight(line: string) {
  if (line.trimStart().startsWith("#")) return <span className="tok-comment">{line}</span>;
  const comment = line.indexOf("  #");
  const code = comment >= 0 ? line.slice(0, comment) : line;
  const parts = code.split(/("[^"]*"|https:\/\/\S+|\benvi(?:-instance)?\b)/g);
  return <>
    {parts.map((part, i) => /^"/.test(part) || /^https:/.test(part)
      ? <span key={i} className="tok-string">{part}</span>
      : /^envi/.test(part) ? <span key={i} className="tok-cmd">{part}</span> : part)}
    {comment >= 0 && <span className="tok-comment">{line.slice(comment)}</span>}
  </>;
}
