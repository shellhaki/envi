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
    date: "2026-11-19",
    version: "v1.6.0",
    title: "Self-hosted first, with Envi Cloud next to it",
    summary: "Envi is now made to run on your own server first. The hosted dashboard is Envi Cloud, at cloud.envisecrets.com, and it keeps its free tier and paid plans.",
    changes: [
      "envisecrets.com now shows how to install your own instance and the CLI.",
      "The dashboard moved to cloud.envisecrets.com. Old links, including invitations, take you there. Sign in once more, since sign-ins belong to the old address.",
      "Nothing changes for the CLI or CI: the API stays at api.envisecrets.com.",
      "`envi auth` asks whether you sign in to Envi Cloud or your own instance, and remembers the answer. `envi auth --cloud` or `envi auth --url <api-url>` skip the question.",
    ],
  },
  {
    date: "2026-11-19",
    title: "The shell hook is gone",
    summary: "Loading secrets when you cd into a folder is removed. `envi run -- <command>` does the same job for one command, with nothing left in your shell afterwards.",
    changes: [
      "`envi hook`, `envi allow`, `envi deny` and `envi reload` are removed.",
      "If you installed the hook, update the CLI with `envi update now`. The next shell you open takes the hook out of its startup file, with one line saying so.",
      "The install script no longer asks about the hook.",
    ],
  },
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
      "`envi set-url <api-url>` points the CLI at your instance; `envi set-url --reset` points it back at Envi Cloud.",
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
