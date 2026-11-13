type DocPage = { slug: string; title: string; description: string };
export type DocGroup = { group: string; pages: DocPage[] };

/** Where the main site lives. This app is its own deployable on a docs
 *  subdomain, so links back out are absolute. */
export const SITE_URL = process.env.NEXT_PUBLIC_SITE_URL ?? "https://envisecrets.com";

/**
 * Single source of truth for the docs: sidebar order, page headers, and the
 * prev/next footer links all read from here, so an .mdx file only ever holds
 * body content.
 */
export const docsNav: DocGroup[] = [
  {
    group: "Get Started",
    pages: [
      { slug: "/", title: "Introduction", description: "Environment secrets, encrypted, scoped, and audited." },
      { slug: "/installation", title: "Installation", description: "Install the envi CLI on macOS, Linux, Termux, or Windows." },
      { slug: "/quickstart", title: "Quickstart", description: "Authenticate, create a project, and push your first .env." },
    ],
  },
  {
    group: "Concepts",
    pages: [
      { slug: "/concepts/projects-and-environments", title: "Projects & Environments", description: "How secrets are organized." },
      { slug: "/concepts/secrets-and-encryption", title: "Secrets & Encryption", description: "How values are stored, versioned, and read." },
      { slug: "/concepts/access-and-permissions", title: "Access & Permissions", description: "Read, write, manage — and why production is different." },
      { slug: "/concepts/key-value-store", title: "Key-Value Store", description: "Project-wide values your application owns, beside your secrets." },
    ],
  },
  {
    group: "Guides",
    pages: [
      { slug: "/cli", title: "CLI Reference", description: "Every envi command." },
      { slug: "/web-dashboard", title: "Web Dashboard", description: "Manage projects and secrets visually." },
      { slug: "/docker", title: "Docker", description: "Run your app in a container with no .env and one credential." },
      { slug: "/ci-service-tokens", title: "Service Tokens & CI/CD", description: "Pull secrets in a pipeline without a human's session." },
      { slug: "/invitations", title: "Invitations", description: "Invite a collaborator, even if they don't have an account yet." },
    ],
  },
  {
    group: "Self-Hosting",
    pages: [
      { slug: "/self-hosting", title: "Install an instance", description: "Your own Envi server, free, in one command." },
      { slug: "/self-hosting/users", title: "Users and passwords", description: "Add people, give them projects, reset a password." },
      { slug: "/self-hosting/cli", title: "Connect the CLI", description: "Point envi and your CI at your instance." },
      { slug: "/self-hosting/operations", title: "Run the instance", description: "Status, logs, backups, restores and updates." },
    ],
  },
];

const flatDocs: DocPage[] = docsNav.flatMap((g) => g.pages);

export function findDoc(pathname: string) {
  // Pages are built at /docs/cli but served at docs.envisecrets.com/cli, so accept both.
  // Trailing slashes would otherwise miss every lookup.
  const path = pathname.replace(/^\/docs(?=\/|$)/, "") || "/";
  const slug = path !== "/" && path.endsWith("/") ? path.slice(0, -1) : path;
  const index = flatDocs.findIndex((p) => p.slug === slug);
  if (index === -1) return null;
  return {
    page: flatDocs[index],
    prev: index > 0 ? flatDocs[index - 1] : null,
    next: index < flatDocs.length - 1 ? flatDocs[index + 1] : null,
  };
}
