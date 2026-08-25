# Envi — Phased Architecture & Implementation Plan

## 0.1 Note on target user (updated)

Envi's primary user is not just a team — it's an **individual developer** first, who may later invite collaborators. The flow that has to feel effortless is:

1. Solo dev builds something locally, runs `envi init` + `envi push`.
2. Deploys to their own server (VPS, droplet, whatever) and runs `envi pull` there to get the `.env` populated — no teammate, no org setup, no RBAC decisions required.
3. Later, if someone joins the project, the owner shares access to that one project without needing to first stand up an "organization" with roles and seats.

This means **org/team structure must be optional scaffolding, not a mandatory first step.** Concretely:
- Every user gets an implicit **personal workspace** (a lightweight, auto-created "org of one") the moment they sign up — no "create your organization" screen blocking `envi init`.
- A project can be created directly under a personal workspace. "Real" multi-person organizations become relevant only once someone shares a project or explicitly creates a team.
- **Sharing a project = inviting a collaborator to that specific project**, not necessarily onboarding them into a full org hierarchy. Under the hood this can still be implemented as "invite user, create an access_grant scoped to that project" — the org/team machinery from §1.3 doesn't disappear, it just becomes invisible until it's needed.
- **Authenticating a server** (as opposed to a developer's laptop) is a distinct flow from the interactive device-code auth in §1.5 — see §1.5.1 below. This is what makes "push locally, pull on my server" actually work unattended.

This reframing affects §1.4 (RBAC), §1.5 (auth), and the MVP scope — see the additions marked "(updated)" below.

## 0.2 Open source, licensing, and pricing (new)

**Model: open-core.** The core Envi codebase (API, CLI, dashboard) is open source and self-hostable by anyone. You additionally run a hosted instance, and monetize that hosted instance with a paid tier. This is the same shape as GitLab, Plausible, Cal.com, PostHog, etc.

**License choice matters here and is worth deciding deliberately, not defaulting to MIT:**
- Plain **MIT/Apache-2.0** means nothing stops a third party from taking your code, hosting it, and competing with your own hosted offering directly.
- Options actually used by comparable open-core products:
  - **BSL (Business Source License)** — source-available, free for self-hosting/internal use, but restricts *competing hosted-service* use for a set period (e.g. 3–4 years) before auto-converting to a permissive license. Used by Sentry, CockroachDB.
  - **AGPL** — anyone can self-host, but if they modify and offer it as a network service, they must also open-source their modifications. Weaker protection against a competing host than BSL, but simpler and more "purely open source" in spirit.
  - **Fair Source / FSL** — newer, explicitly designed for this exact "open but not free-to-compete-with-me" situation.
- **Recommendation to validate with a lawyer or at least careful reading, not just take my word for it:** BSL or FSL if you want the strongest protection for your hosted business while still being genuinely source-available; AGPL if you'd rather stay squarely inside "traditional open source" and accept the weaker competitive protection.
- Practically: this decision doesn't block Phase 0/1 engineering work at all — it's a `LICENSE` file choice you can finalize any time before public launch. Doesn't need to hold up building.

**Pricing (hosted instance only — self-hosters pay nothing beyond their own infra):**

| Tier | Price | Notes |
|---|---|---|
| Free | $0 | Personal workspace, reasonable secret/project limits, community support |
| Pro | **$5/month**, or **$50/year** (a $10/yr discount vs. paying monthly — i.e. ~2 months free) | Removes free-tier limits; likely gates: unlimited shared collaborators, unlimited projects, longer audit-log retention, priority support |

Implementation notes for later (not MVP-blocking):
- Gate Pro features behind an `organizations.plan` column checked at the API layer, not the client — never trust a client-side flag for a paywall.
- Stripe (Checkout + Billing) is the standard, low-effort way to handle the $5/mo vs $50/yr distinction, proration, and dunning — no need to build billing logic from scratch (this matches "avoid advanced billing logic" in the MVP-scope-avoid list already).
- Self-hosted instances should have Pro-gating code physically present but inert (or entirely absent, depending on license strategy) — decide this alongside the license choice above, since "does the open-source version include the paywall code" is itself a licensing-adjacent decision.

## 0.3 Working with an AI coding assistant (new)

Since you're planning to have an AI (e.g. Claude Code) actually build this, a few working agreements are worth setting up front so you stay in the loop rather than getting a black box handed back to you:

- **Work in the phases already defined below, not all at once.** Ask the AI to implement one phase (or even one numbered item within a phase) per session, and stop for your review before moving to the next. Resist the temptation to say "build the whole MVP" in one prompt — you won't be able to meaningfully review a 5,000-line diff.
- **Require an explanation before/alongside code, not just after.** A good pattern: ask it to first state its plan in plain language (what files it'll touch, what the approach is, and *why*, especially for anything security-sensitive like the encryption or auth code), then implement, then summarize what it actually did and any deviations from the plan.
- **Treat the encryption, auth, and access-control code as the parts you personally read line-by-line**, even if you skim everything else. That's where a subtle AI mistake is most expensive. Everywhere else (CLI plumbing, dashboard UI, email templates) can reasonably get a lighter review pass.
- **Ask it to follow standard, boring security practice, explicitly** — parameterized queries (never string-built SQL), no secrets in logs, dependency pinning, input validation at API boundaries, principle of least privilege for DB roles. Naming these up front in your prompt reduces the chance it takes shortcuts under the (correct) assumption that you just want something working fast.
- **Ask for tests alongside features**, not as a cleanup pass at the end — especially unit tests for the encryption/RBAC logic per §6 below, since those are exactly the areas where "looks right" and "is right" diverge.
- **Data design is explicitly yours.** The schema sketch in §2 below is a reference starting point only — treat it as "here's a shape that would work," not a spec to hand to the AI verbatim. When you do have the AI implement the schema, review its migration/table design against your own before running it, since this is the part of the system you said you want to own.

## 0.4 Tech stack note: Go/Gin for API and CLI (updated)

Given the priority on speed, the plan now assumes **Go with the Gin framework for the API backend, and Go for the CLI** (rather than the Bun/Hono backend in the existing waitlist repo). This is a good fit for this specific product, not just a preference call:

- **CLI**: Go was already the stronger recommendation in the original sketch (§ CLI responsibilities) — a single static binary with no runtime dependency is exactly what you want for `curl | install`-style distribution across macOS/Linux/Windows, and for the unattended-server use case in §1.5.1 (a Go binary drops onto a bare VPS with nothing else to install).
- **API**: Gin is fast, has a small learning curve, and Go's standard library + ecosystem (`crypto/aes`, `golang.org/x/crypto`, official AWS/GCP SDKs) covers everything the encryption model in §1.2 needs without exotic dependencies.
- **Shared code**: using Go for both API and CLI means the encryption/envelope-unwrapping logic, the `.env` parsing, and the API request/response types can live in shared internal packages — one implementation of "how do we decrypt a secret" instead of two (previously: TypeScript on the server, and either Go or TypeScript on the CLI, meaning error-prone reimplementation risk). This is a real correctness win, not just a speed one.
- **Practical effect on Phase 0 below**: this means the existing Bun/Hono backend in the waitlist repo is not the foundation to build on — it gets replaced by a new Go/Gin service. The Next.js frontend and Postgres choice are unaffected and stay as-is. The existing Go `whatsapp` module in the repo suggests some Go tooling/CI may already be partially set up, which is worth reusing where it exists (Dockerfile patterns, CI config, etc.) even though the module itself isn't part of the product.

## 0.5 Waitlist & early access strategy (new)

Direct answer: **yes — waitlist joiners should get first access**, and it's worth being deliberate about what "first" means and what they get beyond just an early invite, since this is effectively your first real product decision that touches both engineering and marketing.

**Recommended shape:**
- **Staged invite waves**, not "everyone in at once." The existing waitlist/newsletter endpoints (repo context, Phase 0) already capture signups — when Phase 1 is functionally ready (roughly once Phase 1-a through Phase 1-g are done: secrets, CLI push/pull, service tokens, sharing, basic audit visibility), invite the waitlist in small batches rather than opening broadly. This does double duty: it's real-world load/UX testing with a friendly audience before a public launch, and it creates the "I got in early" feeling that drives word-of-mouth.
- **Order of invitation**: simplest fair approach is signup order (first joined, first invited), optionally with a manual bump for anyone who's been actively engaged (replied to updates, starred the repo, etc.) — not required for MVP, just worth keeping in mind if the list is large enough to matter.
- **Concrete waitlist benefits worth committing to publicly** (state these plainly when someone joins, so it's a real incentive, not a vague "you'll hear from us"):
  1. **Early access itself** — using the product before public launch/before it's on Show HN or Product Hunt.
  2. **Locked-in founding-member pricing** — e.g. waitlist members who convert to Pro during the early-access window keep the $5/mo·$50/yr rate even if pricing changes later as the product matures. This is a standard, low-cost-to-you, high-perceived-value lever (you're not giving away margin, you're just promising not to raise their price later).
  3. **Direct input on the roadmap** — early users' feedback shapes Phase 2 priorities; worth explicitly inviting this rather than assuming they'll volunteer it.
  4. Optional, if you want a stronger incentive: **a longer or permanently-extended audit-log retention window on Pro** for founding members specifically, as a small lasting perk distinct from pricing.
- **What this requires from engineering** (small, but worth flagging now rather than discovering it during Phase 1-h/i): a simple **invite-code or allowlist gate** on signup during the early-access window (an `invited_at`/`invite_status` column on the waitlist table is enough — no need for anything more complex), and a way to flag an account as "founding member" so the locked-in pricing can be enforced later in Phase 3-d's billing integration. Cheap to add to Phase 0-c (auth) as a small extension, expensive to retrofit after Pro billing is live.
- **Communicate the wave plan to the list itself.** A short "here's how early access will roll out, and what you get for being on this list" email once, early, sets expectations and reduces "why haven't I been invited yet" friction later — pairs naturally with the Phase 1 build-in-public update cadence already noted in the marketing doc's GTM section.

## 0. Guiding principles

- **Never store or log plaintext secrets.** Not in the database, not in application logs, not in error reporting, not in client-side state longer than necessary to render/copy it.
- **Optimize the MVP for trust and convenience, not maximal zero-knowledge purity.** The target user (a startup dev team) wants "safer than Slack," not "safer than a bank." Build the envelope-encryption foundation so a stricter client-side mode can be added later without a data-model rewrite.
- **Treat CLI ergonomics as a product surface, not an afterthought.** `envi auth / init / pull / push` needs to feel as fast as `git`.
- **Every write to a secret is versioned and audited.** No exceptions, including admin actions.

---

## 1. Core architectural decisions (flagged for explicit validation)

These decisions have outsized long-term cost if reversed later. Each should be a short written ADR before implementation starts.

### 1.1 Where does decryption happen?
**Recommendation: server-side decryption for MVP, using envelope encryption backed by a cloud KMS.**

- The API decrypts secret values on demand (for CLI pull, dashboard reveal) and re-encrypts on push.
- Plaintext exists only in-memory on the API server for the duration of the request, and in-memory/on-disk (`.env`) on the developer's machine.
- This keeps the CLI simple, allows the web dashboard to show/copy secrets, and makes key rotation and recovery tractable.
- **Trade-off to be explicit with users about:** Envi's backend is technically capable of decrypting secrets. This is a trust-model decision, not just a technical one — it should be stated plainly in the security docs.
- **Future path:** design the KMS/DEK layering (below) so a "zero-knowledge" project tier — where unwrapping happens client-side via WASM libsodium in the browser and CLI, and Envi's servers only ever handle ciphertext — can be added later without changing the storage schema.

### 1.2 Envelope encryption model
- **KMS master key** (AWS KMS or GCP Cloud KMS, one per deployment region) — never leaves the cloud HSM.
- **Organization Key-Encryption-Key (KEK)**: generated per organization, encrypted ("wrapped") by the KMS master key, stored in DB as ciphertext only.
- **Per-project Data-Encryption-Key (DEK)**: generated per project, wrapped by the org KEK, stored in DB as ciphertext only.
- **Secret values**: encrypted with the project DEK (AES-256-GCM), stored as ciphertext + nonce + auth tag.
- To decrypt a secret: KMS unwraps org KEK → org KEK unwraps project DEK → DEK decrypts secret value. Only the final step touches secret plaintext, and it happens in-process, never persisted.
- **Key rotation**: rotating a DEK re-encrypts only that project's secrets; rotating a KEK re-wraps only DEKs under it. The KMS master key rotates independently via the cloud provider's native rotation. This layering is what makes rotation cheap — it's the main reason to do envelope encryption instead of encrypting directly with a single global key.

### 1.3 Org / team / project / environment modeling
```
Organization
 └── Membership (user, org_role: owner | admin | member)
 └── Project
      └── Environment (e.g. development, staging, production — user-defined, but "production" gets special treatment)
           └── Secret (key name, current version pointer)
                └── SecretVersion (ciphertext, nonce, created_by, created_at, version_number)
```
- Environments are scoped to a project, not global, so "production" in Project A is a distinct grantable resource from "production" in Project B.
- Access control is evaluated at the environment level, not just the project level — this directly satisfies the "dev can't accidentally touch prod" user story.

### 1.4 RBAC model
Two layers, kept intentionally simple for MVP:
- **Org role** (`owner`, `admin`, `member`): governs org-wide administrative actions (billing, inviting members, creating projects).
- **Access grant** (`subject_id`, `project_id`, `environment_id` nullable, `permission`: `read` | `write` | `manage`): explicit grants layered on top. A grant with `environment_id = null` applies to all environments in the project **except** environments flagged `is_production`, which always require an explicit grant. This gives the "no accidental prod access" guarantee without a full policy engine.
- Defer attribute-based policies, custom roles, and approval workflows to a later phase.

**(updated) Personal-workspace default:** every new user is auto-provisioned a personal `organization` row (`type: personal`, `org_role: owner`, single member) at signup — invisible in the UI as a "workspace picker" unless/until the user is also part of a shared org. `envi init` in a fresh repo creates a project under this personal workspace with zero prompts.

**(updated) Project-level sharing without full org onboarding:** `envi share <email> --project foo --env development` (or the dashboard equivalent) does the following under the hood:
- If the invitee doesn't have an account yet, sends an invitation tied to that project (not the org).
- On acceptance, creates an `access_grant` scoped to that project/environment — it does **not** add them as a full member of the owner's personal org (a personal org stays single-member by definition; the system instead promotes the *project* to belong to a lightweight "shared org" transparently, or — simpler for MVP — access_grants are allowed to reference a project owned by another user's personal org directly, provided the grantor has `manage` permission). The simplest MVP-safe implementation: **access_grants can point at a project regardless of which org owns it**, as long as the grantor has `manage` rights on that project. This avoids needing an org-migration step entirely for the common "just add my one collaborator" case, and a full team org remains available for people who want the heavier structure (dashboard: "Convert to Team").

### 1.5 Device authentication (CLI)
Standard **OAuth 2.0 Device Authorization Grant (RFC 8628)**:
1. `envi auth` → CLI calls `POST /device/code` → receives `device_code`, `user_code`, `verification_uri`.
2. CLI displays the short `user_code` and opens the browser to `verification_uri`.
3. User logs into the web dashboard (if needed) and approves the code, scoped to their account/org.
4. CLI polls `POST /device/token` until approved, then receives a short-lived access token + refresh token.
5. Tokens are stored in the OS keychain (macOS Keychain / Windows Credential Manager / libsecret on Linux), never in a plaintext file.
- Access tokens: short-lived (~15 min), refresh tokens: longer-lived, rotated on use, individually revocable from the dashboard ("Devices" list, like GitHub/Vercel CLI patterns).

**1.5.1 (updated) Server / unattended authentication — "push locally, pull on my server":**
Device-code auth assumes an interactive browser, which a headless VPS doesn't have. For the solo-dev deploy flow, Envi needs a **non-interactive credential** distinct from a personal login:
- `envi token create --project foo --env production --permission read` (run locally, while already authenticated) mints a **service token**: a long-lived, scoped, revocable credential tied to a `service_identity` (§1.8), not a human device session.
- On the server: `ENVI_TOKEN=<token> envi pull` (env var or `--token` flag, or a one-line `curl | sh` style installer that prompts once for the token) — no browser, no keychain interaction needed, no polling.
- This is deliberately the **same mechanism** as the CI/CD service tokens in §1.8 — a personal deploy server and a CI runner are the same kind of actor from Envi's point of view (an unattended puller of secrets), so building one code path covers both the "indie dev's droplet" and the "team's GitHub Actions job" use cases.
- Token scope is always project+environment level, never org-wide, so a leaked deploy-server token can't be used to enumerate someone's entire account.

### 1.6 Push/pull conflict resolution
- Optimistic concurrency, git-style: every `envi pull` records the `version_number` last seen per secret.
- `envi push` submits the base version alongside the new value. If the server's current version doesn't match, it's a conflict — push is rejected with a clear diff-style message rather than silently overwritten.
- `envi diff` shows local vs. remote before pushing. Full 3-way merge tooling is a later-phase nicety, not MVP-required.

### 1.7 Audit log tamper-resistance
- Append-only table; the application DB role used by the API has no `UPDATE`/`DELETE` grant on `audit_events`.
- Each event stores a hash of `(event data + previous event hash)`, forming a hash chain per organization — cheap tamper-evidence without extra infrastructure.
- Later phase: periodic export to write-once object storage (S3 Object Lock or equivalent) for external verifiability.

### 1.8 CI/CD secret injection (MVP-light)
- Introduce **service tokens**: machine identities scoped to exactly one project + environment, read-only, no keychain/device flow needed (issued once via dashboard, stored as a CI secret — this is the one place a raw secret still has to live, which is unavoidable and should be called out in docs).
- `envi run -- <command>` injects variables directly into the subprocess environment; no `.env` file is ever written to disk in CI. This alone covers most real CI/CD needs without building provider-specific integrations (GitHub Actions app, etc.) in MVP.

### 1.9 Offline / revoked access
- Local `.env` written by `pull` persists on disk after revocation — Envi cannot reach into a laptop's filesystem. This should be documented as a known limitation, not silently ignored.
- MVP mitigation: revoke tokens immediately server-side so the *next* `pull`/`push` fails; log revocation events prominently in the audit trail so admins know to have the user delete local files.
- Later phase: optional local secret TTL / `envi pull --ttl` that scripts a self-clean.

---

## 2. Database schema (reference sketch only — not prescriptive)

You've said data design is yours to own, so treat everything below as a starting shape to react to or discard, not something to hand an AI verbatim. The main things worth preserving from this sketch, whatever exact schema you land on:
- Secret values live in an insert-only version table, never overwritten in place (this is what makes version history and conflict detection possible).
- Key material (`kek_ciphertext`, `dek_ciphertext`) is always ciphertext at rest — never a column that could plausibly hold plaintext.
- Access control is evaluated per environment, not just per project, so "production" can be gated independently.

```
users                (id, email, name, password_hash?, created_at)
organizations        (id, name, kek_ciphertext, created_at)
memberships           (id, user_id, org_id, role, created_at)
projects              (id, org_id, name, dek_ciphertext, created_at)
environments          (id, project_id, name, is_production, created_at)
secrets                (id, environment_id, key_name, current_version_id, created_at)
secret_versions        (id, secret_id, ciphertext, nonce, auth_tag, version_number, created_by, created_at)
access_grants          (id, subject_user_id, project_id, environment_id NULL, permission, created_at)
device_sessions        (id, user_id, device_code_hash, user_code, status, expires_at)
api_tokens             (id, user_id OR service_identity_id, token_hash, scope, last_used_at, revoked_at)
service_identities     (id, project_id, environment_id, name, created_at)
invitations            (id, org_id, email, role, invited_by, status, expires_at)
audit_events           (id, org_id, actor_id, action, target_type, target_id, metadata, prev_hash, hash, created_at)
```

Key constraints worth calling out to engineering:
- `secret_versions` is insert-only; `secrets.current_version_id` is the only mutable pointer.
- `dek_ciphertext` / `kek_ciphertext` columns never hold plaintext key material — enforce this with code review checklist + a CI secret-scanner rule, not just convention.

---

## 3. API boundaries (MVP)

| Area | Endpoints |
|---|---|
| Auth | `POST /auth/signup`, `/auth/login`, `/auth/magic-link`, `/auth/refresh` |
| Device flow | `POST /device/code`, `POST /device/token`, `POST /device/approve` |
| Orgs | `GET/POST /orgs`, `GET /orgs/:id/members`, `POST /orgs/:id/invitations` |
| Projects | `GET/POST /projects`, `GET /projects/:id/environments` |
| Environments | `POST /projects/:id/environments`, `GET/PATCH /environments/:id` |
| Secrets | `GET /environments/:id/secrets` (pull), `PUT /environments/:id/secrets` (push, batch + conflict-checked), `GET /secrets/:id/versions` |
| Access | `GET/POST/DELETE /projects/:id/access-grants` |
| Audit | `GET /orgs/:id/audit-events` (paginated, filterable) |
| Service identities | `POST /projects/:id/environments/:id/service-identities` |

CLI and web dashboard consume the same API surface — no CLI-only backdoor endpoints, so authorization logic lives in exactly one place.

---

## 4. Phased implementation plan (trackable checklist)

Each phase is broken into lettered sub-tasks. Check items off as you go; treat each letter as roughly "one AI session + your review," per §0.3.

### Phase 0 — Foundation
- [ ] **a. Project structure & tooling.** Go module layout for the API (`/cmd`, `/internal`, shared packages per §0.4), Gin skeleton, linting/formatting config, CI pipeline skeleton (build + test on push). Retire the Bun/Hono backend; keep the Next.js frontend and Postgres as-is.
- [ ] **b. Database setup.** Local Postgres running (via the dev Compose file, see Phase 1-k below, pulled forward for local dev convenience), migration tool wired up (`golang-migrate` or `atlas`), first migration = empty schema. *Schema design itself is yours per your earlier note — this task is just plumbing.*
- [ ] **c. Authentication.** Signup/login (email+password or magic link — decide per open question §7.2), session/token issuance, password hashing, basic email delivery for verification.
- [ ] **d. Personal workspace + project/environment CRUD.** Auto-provisioned personal org on signup (§0.1), project creation, environment creation (with `is_production` flag) — no secrets yet, just the empty structural objects.
- [ ] **e. KMS + envelope encryption primitives, in isolation.** KEK/DEK generation and wrapping/unwrapping against AWS KMS or GCP Cloud KMS (§1.2), with unit tests, *before* wiring into any real feature. This is the highest-stakes code in the system — review it yourself line by line per §0.3.

### Phase 1 — MVP core
- [ ] **a. Secret storage end-to-end.** Wire Phase 0-e's encryption primitives into real create/read/update endpoints for secrets; insert-only version table (§2).
- [ ] **b. CLI: `auth`, `init`, `pull`, `push`.** Device-code flow (§1.5) for `auth`; `init` detects local `.env` and creates a project under the personal workspace; `pull`/`push` against the API.
- [ ] **c. Service tokens for unattended pull.** `envi token create`, and server-side `ENVI_TOKEN=... envi pull` (§1.5.1) — this is core to the "push locally, pull on my server" promise, not deferrable.
- [ ] **d. Project-level sharing.** `envi share <email> --project --env` (§1.4 updated) without requiring full org setup; invitation email flow.
- [ ] **e. Access grants / environment-scoped RBAC.** Production environments require explicit grants; enforce at the API layer on every secret read/write.
- [ ] **f. Secret version history (read-only view).**
- [ ] **g. Basic audit log — logging *and* a dashboard viewer.** Append-only table, no hash-chain yet; covers secret CRUD, invites, access-grant changes. Include a simple (unfiltered, unpaginated-is-fine-for-now) audit log view on the dashboard in this same task — logging events nobody can see yet doesn't satisfy the "who accessed/changed this secret" user story. Filtering, pagination, and hash-chain verification are the Phase 2-b upgrade, not a prerequisite for basic visibility.
- [ ] **h. Terms of Service & Privacy Policy drafted.** Blocking before any paid signup goes live (§8) — can be templated early and refined, doesn't need to wait for launch week.
- [ ] **i. Documentation v1.** Install instructions (all three channels: Homebrew, npm/bun, raw binary), `envi init` quickstart, and a "why does Envi need a KMS" explainer — this is what determines whether anyone outside you can actually use the thing.
- [ ] **j. Self-host Docker Compose stack, with TLS.** Full service set from §5.2 (api/postgres/web/migrate) *plus* a reverse-proxy service (Caddy, for automatic Let's Encrypt certs) — TLS is not a later add-on, it's required for `docker-compose.yml` to be a genuinely usable production artifact for self-hosters. `.env.example` with clear comments.

### Phase 2 — Team & trust hardening
- [ ] **a. CLI: `projects`, `envs`, `logout`.**
- [ ] **b. Audit log dashboard upgrade: hash-chaining, filters, pagination** (§1.7) — Phase 1-g already shipped basic visibility; this hardens it (tamper-evidence) and makes it usable at scale.
- [ ] **c. Conflict detection on push + `envi diff`** (§1.6).
- [ ] **d. Device/session management UI** (list + revoke active CLI sessions and service tokens).
- [ ] **e. Service identities + `envi run` for CI usage** (§1.8) — same mechanism as Phase 1-c, extended to CI runners explicitly.
- [ ] **f. Self-hoster upgrade path.** Semantic versioning on releases, forward-only migrations, `UPGRADING.md` convention (§8) — establish this now, before schema changes pile up.
- [ ] **g. Documentation v2.** Self-hosting guide (Compose walkthrough end-to-end), API reference, sharing/collaboration guide.

### Phase 3 — Security & operational maturity
- [ ] **a. Key rotation tooling** (rotate DEK/KEK on demand + scheduled reminders).
- [ ] **b. Independent security review** of the encryption model and access-control logic specifically — before any public "secure by design" claim.
- [ ] **c. Rate limiting + audit-log anomaly flagging** (e.g., mass secret reads).
- [ ] **d. Billing integration.** Stripe Checkout/Billing for the $5/mo & $50/yr Pro tiers (§0.2); plan enforcement at the API layer, never client-side.
- [ ] **e. Basic observability for your hosted instance.** Error tracking (Sentry or similar), structured logging, uptime/health-check monitoring (§8) — "how do I find out it's down" needs an answer before you have paying customers.
- [ ] **f. Explicit telemetry decision.** If self-hosted instances phone home at all, make it opt-in and documented plainly in the README (§8) — decide and implement together, don't ship a default silently.
- [ ] **g. Alternative KMS path for self-hosters without AWS/GCP** (§8) — e.g. `age`-encrypted local keyfile or Vault transit engine, clearly labeled as a less-managed option.

### Phase 4 — Later / explicitly out of MVP scope
- [ ] Native CI/CD provider integrations (GitHub Actions app, etc.) beyond the generic `envi run` approach.
- [ ] Dynamic/short-lived secrets (database-generated credentials, cloud-provider auto-rotation).
- [ ] SSO/SAML for enterprise orgs.
- [ ] Full policy engine (custom roles, conditional access, approval workflows).
- [ ] Multi-region replication.
- [ ] Client-side/zero-knowledge decryption mode (§1.1 future path).
- [ ] GDPR erasure-vs-audit-log resolution, if not already forced earlier by a real request (§8) — cheap to design early, worth revisiting here at latest.

---

## 5. Deployment strategy

- **API**: containerized Go/Gin service (single static binary in a minimal container image — Go makes this cheap) on Fly.io, Railway, or ECS Fargate — anything with easy horizontal scaling and secrets-at-the-platform-level for its *own* config (KMS credentials, DB URL).
- **Database**: managed Postgres (RDS, Neon, or Supabase) with automated backups and point-in-time recovery — critical given `secret_versions` is the source of truth.
- **KMS**: AWS KMS or GCP Cloud KMS, matched to whichever cloud hosts the DB, to keep IAM boundaries simple.
- **Frontend**: Vercel (already the natural fit for the existing Next.js app).
- **CLI distribution**: Go's cross-compilation makes this straightforward — build signed static binaries for macOS/Linux/Windows via GoReleaser, distribute via Homebrew tap + `go install` + signed GitHub releases; checksum/signature verification documented so users aren't just `curl | bash`-ing an unsigned script.

### 5.1 CLI via npm/bun despite being a Go binary (updated)

This is a solved pattern, used by tools like esbuild, swc, and Turborepo — worth following the same shape rather than inventing your own:

- The Go CLI still compiles to native platform binaries via GoReleaser (as above) — that doesn't change.
- Publish a **thin npm package** (`envi-cli` or similar) whose `postinstall` script detects the user's OS/architecture and downloads the matching prebuilt binary from GitHub Releases (or fetches it from a small set of **optionalDependencies** platform-specific packages, e.g. `@envi/cli-darwin-arm64`, `@envi/cli-linux-x64` — the approach esbuild uses, which lets npm/bun's own resolution pick the right one instead of a postinstall network call, and works offline in CI with a lockfile).
- The published npm package's `bin` entry just execs the downloaded native binary — no Node.js runtime involvement at actual CLI runtime, so this doesn't cost you any of the Go speed/simplicity, it's purely a distribution convenience layer for people who reach for `npm i -g` / `bunx` out of habit.
- Keep the Homebrew tap and raw GitHub-release binaries as the "no Node ecosystem at all" install path (important for the plain VPS / unattended-server case in §1.5.1, where you don't want to require Node just to install the CLI) — npm/bun becomes an additional, not the only, distribution channel.

---

## 5.2 DevOps: Docker Compose for self-hosting (new)

Since Envi is open source and self-hostable (§0.2), Docker Compose is the right primary artifact for that audience — it's the de facto standard for "clone this repo and run one command" self-hosted tools (Plausible, Cal.com, etc. all ship this way), separate from however *you* deploy your own hosted instance.

**Recommended `docker-compose.yml` service shape:**
```
services:
  api:        # Go/Gin binary in a minimal image (multi-stage build: golang:alpine builder → scratch/distroless runtime)
  postgres:   # official postgres image, named volume for data persistence
  web:        # Next.js dashboard, built as a standalone/production image
  migrate:    # one-shot init container running DB migrations before api starts (depends_on with a healthcheck, not a fixed sleep)
```
- **KMS stays external, deliberately.** Self-hosters still need *a* KMS — don't try to bundle a fake local one for "convenience," since that would silently weaken the encryption model in §1.2 for anyone who doesn't notice. Support both AWS KMS and GCP Cloud KMS via env-var config, and document that a real cloud KMS account is a hard requirement, not optional, even for self-hosting. (A local dev-only mode using a software-simulated KMS, clearly labeled as insecure and never for production, is reasonable for the `docker-compose.dev.yml` developer-experience path — just not the default.)
- **Config via `.env` at the compose level** (ironic, but standard) for DB credentials, KMS provider/keys, SMTP for invitation emails, and the app's own signing secrets — ship a `.env.example` with clear comments, since this is the first thing a self-hoster edits.
- **Two compose files**, following common practice: `docker-compose.yml` (production-shaped: no source mounts, built images, restart policies) and `docker-compose.dev.yml` (hot-reload volume mounts, exposed DB port for local inspection, the insecure local-KMS-simulator mentioned above).
- **Your own hosted instance** doesn't have to run via Compose at all — it can still use the Fargate/Fly.io/managed-Postgres setup from §5, since Compose here is about giving self-hosters a good single-machine experience, not dictating your own production topology.
- **Migrations**: a single `migrate` service/job run via a standard tool (e.g. `golang-migrate` or `atlas`, both Go-native and a natural fit given the stack) keeps schema changes reproducible for self-hosters who `git pull` and re-run compose, not just for your own deploys.

---

## 6. Testing strategy

- **Encryption/decryption and RBAC evaluation**: the highest-value code in the system — target near-100% unit test coverage, including negative cases (wrong key, expired grant, cross-org access attempts).
- **Integration tests**: full CLI ↔ API flows — device auth, pull, push, conflict scenarios — run against a real (test) Postgres and a KMS mock/local equivalent.
- **Security-specific CI checks**: secret-scanning on the repo itself, dependency vulnerability scanning, and a lint rule that fails the build if plaintext secret material appears in a log statement.
- **Load testing**: pull/push endpoints, since these are the CLI's hot path and will be called on every `envi pull` in a CI job.
- **Pre-GA**: independent security review of the encryption model and access-control logic specifically (not a generic pen test) before making any "secure by design" marketing claims.

---

## 7. Open questions to resolve before Phase 1 starts

1. Confirm cloud provider (affects KMS choice and hosting).
2. Email/password vs. magic-link vs. OAuth-only for initial user auth — affects Phase 0 scope.
3. Exact definition of "production" environment protection — is it purely access-grant-based, or does it eventually need MFA-gated reveal, even in MVP?
4. Pricing/seat model shape, since it affects whether `memberships` needs a "billing role" distinct from `access role` sooner than Phase 3.

---

## 8. Gaps not yet covered (worth deciding before/during build)

Things the plan hasn't addressed that will matter in practice:

- **Reverse proxy / TLS for self-hosted Compose.** §5.2's service list has no ingress — a self-hoster running `docker-compose up` on a VPS has no HTTPS. Add a `caddy` or `traefik` service to the stack with automatic Let's Encrypt certs; Caddy is the lower-friction choice for a "clone and run" audience. This is a real functional gap, not a nice-to-have.
- **Legal basics for the paid hosted tier.** Taking $5/mo payments means you need a Terms of Service and Privacy Policy before launch, and if any EU customers subscribe, VAT handling (Stripe Tax covers the calculation/remittance mechanics, but you still need the policy documents). This is boring but blocking — worth templating early rather than scrambling at first paying customer.
- **GDPR/right-to-erasure vs. the append-only audit log.** §1.7 makes audit events tamper-evident and effectively permanent, which sits in tension with "delete my data" requests some jurisdictions require. Worth deciding now whether erasure requests scrub PII from audit metadata while preserving the hash chain's structural integrity, or whether audit retention has a hard TTL. Cheap to design in from day one, painful to retrofit.
- **Self-hoster upgrade path.** Nothing yet defines how someone running Compose safely goes from `v1.2` to `v1.3` when the schema changes. Standard answer: semantic versioning on releases, migrations that only ever run forward (never destructive without an explicit flag), and release notes that call out breaking changes plainly. Worth a short `UPGRADING.md` convention from the first tagged release, not after v5.
- **KMS dependency is a real barrier for the self-host audience specifically.** §1.2/§5.2 assume AWS KMS or GCP Cloud KMS — fine for your hosted instance, but an indie self-hoster on a bare VPS may not have either. Worth deciding whether to support a self-hostable alternative (e.g., a local encrypted keyfile + `age`, or integration with HashiCorp Vault's transit engine) as a documented, clearly-labeled-as-less-managed option, so "must have an AWS account" isn't an unstated requirement for the exact solo-dev audience you're targeting.
- **Basic observability for your hosted instance.** No mention yet of error tracking (Sentry or similar), structured logging, or uptime/health checks. Doesn't need to be sophisticated for MVP, but "how do I find out my hosted API is down before a customer tells me" should have an answer before launch.
- **Anonymous telemetry from self-hosted instances — decide explicitly, don't default silently.** Common in open-core products (opt-in ping with version + rough usage counts, to gauge self-host adoption) but it's a point of real community sensitivity if it ships opt-out or undocumented. Whatever you choose, state it plainly in the README.
- **Documentation site.** Not part of the architecture per se, but adoption for an open-source CLI tool lives or dies on docs (install steps, `envi init` walkthrough, self-host guide, API reference). Worth budgeting for even a simple docs site (e.g. a `/docs` route on the Next.js app, or a static site generator) alongside Phase 1, not after.
- **CLI config file / shell completions.** Small, but expected CLI ergonomics: a project-level config file (e.g. `.envi.toml` or similar, separate from the `.env` it manages) for things like which environment is "current," plus `envi completion bash/zsh/fish`. Cheap to add, easy to forget until users ask.

---

## 9. Go-to-market (new — genuinely missing until now)

Everything above is build-side. None of it matters if nobody finds Envi. This is a smaller topic than the architecture but worth a deliberate pass, not an afterthought after Phase 3.

### 9.1 Positioning & differentiation
- You're entering a space with existing players — Doppler, Infisical (also open-core, closest direct comparison), EnvKey, HashiCorp Vault, 1Password's secrets tooling, and the low-effort baseline of dotenv-vault. Worth writing down, honestly, why an indie dev picks Envi over Infisical specifically, since it's the nearest open-core competitor. Candidate angles based on what you've described: **radically simple solo-dev-first UX** (`envi init/pull/push` with zero org-setup friction, §0.1), **Go-native speed and single-binary install**, and **$5/mo flat pricing** as a clean undercut of per-seat enterprise pricing models. Pick 1–2 of these to lead with, not all of them — trying to be "simple AND fastest AND cheapest AND most secure" in one sentence dilutes all four claims.
- Write this positioning down explicitly (even just a paragraph) before touching launch content — it should drive your README's first paragraph, your landing page hero copy, and your Product Hunt tagline, so they're all saying the same thing.

### 9.2 Pre-launch (during Phase 1 build)
- **Build in public**, if that fits your style — indie-hacker and dev-tool audiences respond well to progress threads (X/Twitter, or a devlog on the docs site). Low cost, and doubles as early feedback from exactly the people you're building for.
- **Waitlist you already have** (the existing repo has waitlist/newsletter endpoints) — use it. Warm that list with a couple of updates before launch rather than going silent until day one.
- **Seed a couple of relevant communities early**, not just at launch: r/selfhosted, r/opensource, indie hacker Discords/Slacks. Answering "how do people currently manage secrets" threads organically, before you're selling anything, builds credibility you can't buy at launch.

### 9.3 Launch
- **Show HN / Hacker News** and **Product Hunt** are the two standard channels for exactly this product shape (open-source dev tool). HN specifically rewards technical depth in the post itself — a short writeup on the envelope-encryption model (§1.2) as a "Show HN: Envi — open-source secrets manager, here's how the encryption works" post plays to genuine strength and isn't just marketing fluff.
- **GitHub README is your real landing page** for the OSS audience — badges, a clear quickstart, an architecture diagram, and a comparison table against the competitors named in §9.1 (factual, not disparaging) all measurably affect adoption for CLI tools.
- **Dev-focused content**: a "why we built Envi" post, a "Doppler/Infisical vs Envi" honest comparison, and a "how envelope encryption works" technical explainer all serve double duty as marketing and documentation (§8/Phase 1-i) — write these once, use in both places.

### 9.4 Ongoing / community (post-launch)
- **A community channel** (GitHub Discussions is the lowest-overhead choice for an OSS project; Discord if you want higher-touch engagement) — needed regardless, since self-hosters will have setup questions your docs won't fully anticipate.
- **Conversion funnel from free → Pro** needs at least basic tracking (which free-tier limit actually causes people to upgrade?) — ties back to the observability gap in §8, worth instrumenting from the start rather than guessing later.
- **Changelog / release notes as a marketing surface**, not just a git log — ties directly into the self-hoster upgrade path (Phase 2-f) and doubles as ongoing "we're still shipping" signal.

### 9.5 Other non-technical gaps worth naming alongside marketing
- **Name/trademark/domain check.** Worth a quick search to confirm "Envi" isn't already a registered trademark or in heavy use by another dev tool, before it's on a README, a domain, and a Product Hunt listing.
- **Support expectations for a solo-founder OSS+SaaS product.** Decide up front what response time you're implicitly promising Pro subscribers vs. free self-hosters (e.g., "Pro gets email support, self-host is community/GitHub-Issues-only") so it's a stated policy, not an accidental one discovered under pressure.