export type DocPage = { slug: string; title: string; description: string };
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
    ],
  },
  {
    group: "Guides",
    pages: [
      { slug: "/cli", title: "CLI Reference", description: "Every envi command." },
      { slug: "/web-dashboard", title: "Web Dashboard", description: "Manage projects and secrets visually." },
      { slug: "/ci-service-tokens", title: "Service Tokens & CI/CD", description: "Pull secrets in a pipeline without a human's session." },
      { slug: "/invitations", title: "Invitations", description: "Invite a collaborator, even if they don't have an account yet." },
    ],
  },
  {
    group: "Self-Hosting",
    pages: [
      { slug: "/self-hosting", title: "Overview", description: "What running your own instance actually involves." },
      { slug: "/self-hosting/docker", title: "Docker Compose", description: "The whole stack in one command." },
      { slug: "/self-hosting/deployment", title: "Deployment", description: "A first deploy to a plain VPS, no Docker required." },
    ],
  },
];

export const flatDocs: DocPage[] = docsNav.flatMap((g) => g.pages);

export function findDoc(pathname: string) {
  // Trailing slashes would otherwise miss every lookup.
  const slug = pathname !== "/" && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  const index = flatDocs.findIndex((p) => p.slug === slug);
  if (index === -1) return null;
  return {
    page: flatDocs[index],
    prev: index > 0 ? flatDocs[index - 1] : null,
    next: index < flatDocs.length - 1 ? flatDocs[index + 1] : null,
  };
}
