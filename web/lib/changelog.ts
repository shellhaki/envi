// What changed in Envi, newest first. Shown at envisecrets.com/changelog.
// Add an entry at the top for every change people would notice.

export type ChangelogEntry = {
  date: string; // YYYY-MM-DD
  version?: string; // the CLI release, when there is one
  title: string;
  summary: string;
  changes: string[];
};

export const CHANGELOG: ChangelogEntry[] = [
  {
    date: "2026-11-14",
    version: "v1.5.0",
    title: "Self-host Envi on your own server",
    summary: "Envi now runs on a server you own, with everything free: no billing, no email provider, no limit on people or projects. One command installs it.",
    changes: [
      "Install an instance with `curl -fsSL https://install.envisecrets.com | sh -s instance`. The same link still installs the CLI.",
      "Choose how people reach it: a domain with HTTPS from Let's Encrypt, a domain behind the reverse proxy you already run, or the server's IP on a private network.",
      "Docker keeps the instance to itself on two private networks. Postgres and Redis open no ports on the server. pm2 is there for servers without Docker.",
      "People sign in with an email and password. The admin adds people on the new Users page, gets a password to hand them, and picks which projects each one sees.",
      "A forgotten password is reset by writing a new one into the users file. Nothing is emailed.",
      "`envi set-url <api-url>` points the CLI at your instance; `envi set-url --reset` points it back at envisecrets.com.",
      "`envi-instance` looks after the server: status, logs, restart, backups and updates.",
    ],
  },
  {
    date: "2026-11-12",
    title: "A faster API, closer to you",
    summary: "The API moved to Cloudflare Workers, with the database on Neon and a Redis cache on Upstash. Nothing changes for you: the same address, the same CLI, the same data.",
    changes: [
      "api.envisecrets.com now answers from Cloudflare's network instead of a single server.",
      "The docs now live at docs.envisecrets.com, served by the same site as the dashboard.",
      "install.envisecrets.com is served by Cloudflare too.",
      "The npm package is no longer maintained. Install and update the CLI with the install script, which also works in Docker images and CI.",
    ],
  },
  {
    date: "2026-10-17",
    version: "v1.4.0",
    title: "Secrets load when you cd into a project",
    summary: "The shell hook loads a project's secrets into your shell as you enter its folder, and unloads them as you leave. Nothing is written to disk.",
    changes: [
      "`envi hook install` sets it up for zsh, bash, fish or PowerShell.",
      "`envi allow` lets a folder load its secrets; production needs `envi allow --production`. `envi deny` stops it.",
      "`envi reload` fetches the secrets again at your next prompt.",
    ],
  },
  {
    date: "2026-10-13",
    version: "v1.3.0",
    title: "Copy secrets to the clipboard",
    summary: "`envi copy` puts secrets on your clipboard without writing a file.",
    changes: [
      "`envi copy` copies every secret as .env lines; `envi copy KEY` copies one value.",
      "`envi copy --single` lets you pick one from a list.",
    ],
  },
  {
    date: "2026-10-02",
    version: "v1.2.0",
    title: "Delete a local .env safely",
    summary: "`envi clean` deletes your local .env only once Envi already holds the same secrets, so nothing is lost.",
    changes: [
      "`envi clean` checks the server first and refuses if the file and the server differ.",
      "`envi push --clean` pushes, then deletes the file.",
    ],
  },
];
