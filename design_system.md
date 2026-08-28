# Envi — Design System

Reference: the existing waitlist page (near-black background, acid-yellow accent, glass pill nav, bold rounded display type). This document systematizes that identity into reusable tokens and components, and extends it to light mode and the rest of the product (dashboard, docs, CLI-adjacent surfaces).

---

## 0. Brand foundation

**Personality:** confident, fast, plainspoken. Envi is a developer tool, not a consumer SaaS product — the design should feel like it was made by people who use a terminal daily, not like a generic dashboard template. Bold where it counts (headlines, primary actions), quiet everywhere else.

**The signature device — the terminal chip.** Every product in this space (Doppler, Infisical, EnvKey) reaches for the same visual vocabulary: dark background, single bright accent, rounded cards. To avoid Envi looking like "any of them," one motif should recur wherever the brand wants to be memorable: a small pill- or rounded-rect-shaped **mini terminal prompt** — monospace type, a literal `$ envi pull` or `$ envi push`, optional blinking cursor — used in the hero, empty states, onboarding steps, and docs. It's not decoration; it's the product's actual daily ritual, rendered as a UI element. Spend this device deliberately, not everywhere — one strong placement per screen beats five small ones.

**Wordmark & mark:** lowercase `envi` wordmark, bold/black weight, tight tracking, matching the display type below. The rounded creature mark stays simple — a single filled shape in the accent color, no gradients or drop shadow, so it works at 16px (favicon) and 200px (hero) without redrawing.

---

## 1. Color

### 1.1 Dark mode (primary — matches the existing waitlist)

| Token | Hex | Usage |
|---|---|---|
| `--bg` | `#0A0A08` | Page background — near-black with a warm (not blue) undertone |
| `--bg-elevated` | `#141410` | Cards, modals, dropdowns |
| `--bg-elevated-2` | `rgba(20,20,16,0.6)` | Glass surfaces — nav bar, floating toolbars (pair with `backdrop-filter: blur(16px)`) |
| `--border` | `rgba(255,255,255,0.08)` | Default card/input borders |
| `--border-strong` | `rgba(255,255,255,0.16)` | Hover states, active/focus borders |
| `--text-primary` | `#FAFAF5` | Headings, primary body text |
| `--text-secondary` | `#A8A89C` | Supporting text, nav links |
| `--text-tertiary` | `#6B6B60` | Placeholder text, disabled, timestamps |
| `--accent` (Envi Yellow 500) | `#F5E70A` | Primary buttons, links-as-actions, logo, active nav state, focus glow |
| `--accent-hover` (Yellow 600) | `#D9CC00` | Hover/pressed state for accent-filled elements |
| `--accent-tint` (Yellow 400) | `#F9EF52` | Subtle glow, decorative background lines, icon fills on hover |
| `--accent-subtle` (Yellow 100, low-opacity use) | `rgba(245,231,10,0.12)` | Background tint behind accent-adjacent content (e.g. behind the hero card) |

### 1.2 Light mode

Not an inversion — light mode keeps the same warm undertone so the brand temperature stays consistent, and pulls the yellow back to an accent role rather than a large fill (yellow-on-white text is close to unreadable; yellow only ever appears as a filled shape with black text on top, never as text-on-light-background).

| Token | Hex | Usage |
|---|---|---|
| `--bg` | `#FAF9F4` | Page background — warm off-white, not stark white |
| `--bg-elevated` | `#FFFFFF` | Cards, modals |
| `--bg-elevated-2` | `rgba(255,255,255,0.72)` | Glass nav (pair with `backdrop-filter: blur(16px)`) |
| `--border` | `rgba(10,10,8,0.08)` | Default borders |
| `--border-strong` | `rgba(10,10,8,0.16)` | Hover/active borders |
| `--text-primary` | `#0E0E0C` | Headings, primary text |
| `--text-secondary` | `#55554C` | Supporting text |
| `--text-tertiary` | `#8B8B80` | Placeholder, disabled |
| `--accent` | `#EDD400` (slightly deepened from dark mode's `#F5E70A` for AA contrast as a fill against white) | Primary button fill, active states |
| `--accent-hover` | `#D4BD00` | Hover/pressed |
| `--accent-tint` | `#FBF6C4` | Subtle background wash (e.g. behind a "Beta" badge) |
| `--accent-subtle` | `rgba(237,212,0,0.10)` | Background tint |

**Rule, not a suggestion:** text is never rendered in the accent yellow directly on `--bg` in light mode. Yellow is always a filled shape (button, badge, tag) with `--text-primary`-on-dark or pure black text on top of it. In dark mode, accent-as-text is fine (yellow on near-black has excellent contrast) and used sparingly for links/active states.

### 1.3 Semantic colors (both modes — deliberately distinct from the brand accent)

The brand yellow is reserved for *interactive/brand* meaning. Status meaning uses its own palette so a "production" warning is never visually confused with "click this button."

| Token | Dark | Light | Usage |
|---|---|---|---|
| `--success` | `#34D399` | `#0F9D63` | Confirmations, "active" status dots |
| `--warning` | `#F5A623` | `#B5780A` | Non-blocking warnings |
| `--danger` | `#F1503C` | `#C7331F` | Destructive actions, production-environment tagging (see §7.5) |
| `--info` | `#5B9DF9` | `#2563EB` | Informational only, used sparingly |

---

## 2. Typography

Three roles, deliberately not the same face doing double duty — this is the single biggest lever for not reading as a generic template.

| Role | Typeface | Weight(s) | Where |
|---|---|---|---|
| **Display** | Cabinet Grotesk | 800 / 900 | Headlines, hero copy, the wordmark, stat numerals — chunky, rounded terminals, matches the existing waitlist's bold rounded feel |
| **Body** | Inter | 400 / 500 / 600 | All UI text, paragraphs, labels, nav |
| **Mono** | JetBrains Mono | 400 / 500 | The terminal-chip signature device, code blocks, CLI flag references in docs, secret keys/values display |

### Type scale

| Token | Size / Line height | Weight | Tracking | Use |
|---|---|---|---|---|
| `display-xl` | 64px / 1.05 | 900 | -2% | Hero headline |
| `display-l` | 44px / 1.1 | 800 | -1.5% | Section headers |
| `display-m` | 32px / 1.15 | 800 | -1% | Card/panel titles |
| `heading-l` | 24px / 1.25 | 700 | 0 | Subsection headings |
| `heading-m` | 20px / 1.3 | 600 | 0 | Component titles (card headers, modal titles) |
| `body-l` | 18px / 1.55 | 400 | 0 | Lead paragraphs |
| `body-m` | 16px / 1.55 | 400 | 0 | Default body text |
| `body-s` | 14px / 1.5 | 400 | 0 | Secondary text, table cells |
| `label` | 12px / 1.4 | 600 | +8% (uppercase) | Micro-labels — matches the countdown timer's "DAYS / HOURS" treatment; reuse this exact pattern for stat labels elsewhere (dashboard KPIs) |
| `mono-m` | 14px / 1.5 | 500 | 0 | Code, terminal chips, secret values |
| `mono-s` | 13px / 1.4 | 400 | 0 | Inline code references |

---

## 3. Spacing, radius, layout

**Spacing scale (4px base):** `4, 8, 12, 16, 24, 32, 48, 64, 96, 128`

**Radius scale:**

| Token | Value | Use |
|---|---|---|
| `--radius-sm` | 8px | Small inline elements (tags in tables) |
| `--radius-md` | 12px | Standard form inputs (dashboard forms — not marketing pills) |
| `--radius-lg` | 20px | Standard cards |
| `--radius-xl` | 28px | Hero/feature cards — matches the waitlist card |
| `--radius-full` | 9999px | Pills: nav bar, primary buttons, badges, marketing-page inputs, avatars |

**Layout:** 12-column grid, max content width 1200px, gutters 24px (desktop) / 16px (mobile). The floating glass nav bar sits centered with margin, not full-bleed — matches the waitlist reference exactly; keep that as the standard nav treatment across the whole product, including the dashboard.

---

## 4. Elevation & glow

Dark backgrounds make conventional drop shadows invisible — Envi uses **border + glow** instead of shadow for elevation in dark mode, and soft warm-tinted shadows in light mode.

| Token | Dark mode | Light mode | Use |
|---|---|---|---|
| `--elevation-card` | `1px solid var(--border)` (no shadow) | `0 1px 2px rgba(20,15,0,0.04), 0 4px 16px rgba(20,15,0,0.06)` | Standard cards |
| `--elevation-glow-accent` | `0 0 60px rgba(245,231,10,0.12)` | `0 0 40px rgba(237,212,0,0.08)` | Behind hero/primary cards only — one per screen, not every card (restraint) |
| `--elevation-modal` | `1px solid var(--border-strong)` + `0 20px 60px rgba(0,0,0,0.5)` | `0 20px 60px rgba(20,15,0,0.15)` | Modals, dropdowns |

The decorative ambient wavy lines from the waitlist background are a **marketing-page-only** device — don't carry them into the dashboard, where they'd compete with real data. Reserve them for landing/marketing surfaces.

---

## 5. Iconography

- Outline style, 1.5px stroke, 20px/24px grid — [Lucide](https://lucide.dev) icon set (already available in this environment's React components) matches the rounded-geometric brand feel without extra licensing.
- Icons are `--text-secondary` at rest, `--text-primary` or `--accent` on hover/active — never filled/solid icons except status dots.
- The mascot mark is the only illustrated brand element; don't introduce a second illustration style (no isometric spot illustrations, no gradient blobs) — keep visual novelty concentrated in the terminal-chip device and the mark, per the "one signature element" principle.

---

## 6. Motion

- **Page load:** hero elements fade + rise 12px, staggered ~60ms apart. Once, on load — not repeated on scroll for every section.
- **Hover (buttons, cards):** scale to 1.02 + brightness/border shift, 150ms ease-out. Subtle, not bouncy.
- **Ambient background glow lines:** slow drift, 20s+ loop, low amplitude — atmosphere, not attention-grabbing.
- **Terminal chip cursor blink:** 1s step interval, matches real terminal cursor behavior — this is the one place a slightly "busier" animation is justified, since it's mimicking the actual product.
- **`prefers-reduced-motion: reduce`:** disable ambient drift, hover scale, and cursor blink; keep opacity-only fades. Non-negotiable.

---

## 7. Components

### 7.1 Navigation
Floating pill container, `--bg-elevated-2` + blur, `--radius-full`, centered with margin from viewport edges (not full-bleed). Logo + wordmark left, links center, primary CTA right. Active/current-section state uses a filled accent pill (matches the "Beta" badge treatment in the reference) rather than an underline — underlines read as generic; the filled pill is already the brand's established pattern.

### 7.2 Buttons

| Variant | Fill | Text | Border | Hover |
|---|---|---|---|---|
| Primary | `--accent` | Black (`#0E0E0C`, both modes — the brand's signature high-contrast pairing) | none | `--accent-hover` + scale 1.02 |
| Secondary | transparent | `--text-primary` | `1px solid var(--border-strong)` | bg tint `--accent-subtle` at 4% |
| Ghost | transparent | `--text-secondary` | none | text → `--text-primary` |
| Destructive | transparent | `--danger` | `1px solid var(--danger)` at 40% opacity | fill `--danger` at 8% |

All buttons: `--radius-full`, padding `12px 24px` (default) / `8px 16px` (small). Icon-only buttons are circular, ghost by default.

**Focus ring (all interactive elements, both modes):** 2px solid, 2px offset. On a yellow-filled button specifically, the ring must be a **dark** ring (`--text-primary` in dark mode is near-white — use `#0E0E0C` instead, a fixed dark ring color, since a yellow ring on a yellow button is invisible). Everywhere else, ring color is `--accent`.

### 7.3 Inputs

Two shapes, used contextually — don't let the marketing page's pill input leak into dense dashboard forms, and don't let boxy dashboard inputs leak into marketing:

- **Marketing/landing inputs** (email capture, waitlist): `--radius-full`, matches the reference exactly — leading icon slot, `--text-tertiary` placeholder.
- **Product/dashboard inputs**: `--radius-md`, standard rectangular field — higher information density needed in forms with many fields (project creation, access grants) than a pill shape comfortably allows.
- **Focus state (both):** border → `--accent`, plus a 3px `--accent-subtle` glow ring.
- **Mono inputs** (secret values, API keys): `mono-m` type, with a masked/reveal toggle icon — these are the one input type that should visually signal "this is sensitive," e.g. a subtle `--danger`-tinted left border when revealed.

### 7.4 Badges & pills

- **Status badges** ("Beta," "New," "Pro"): filled `--accent`, black text, `label` typography, `--radius-full` — exact match to the reference "Beta" pill.
- **Neutral tags**: `--bg-elevated` fill, `1px solid var(--border)`, `--text-secondary` text.

### 7.5 Environment tags (product-specific — ties design directly to the security model)

Since Envi's whole premise is "don't accidentally touch production," the environment tag shown next to any project/secret should carry real visual weight, not just be a neutral label:

| Environment | Fill | Text |
|---|---|---|
| Development | `--bg-elevated` / neutral border | `--text-secondary` |
| Staging | `--info` at 12% fill | `--info` |
| Production | `--danger` at 14% fill, `1px solid var(--danger)` at 40% | `--danger` |

A production-tagged row in a table also gets a 2px `--danger` left border on the row itself — the goal is that a user scanning a list of secrets should be able to tell "this one's production" from peripheral vision alone, before reading the label.

### 7.6 Cards
`--radius-xl` for hero/feature cards, `--radius-lg` for standard content cards. `--elevation-card` by default; `--elevation-glow-accent` reserved for exactly one hero card per screen.

### 7.7 Stat / countdown blocks
Reuse the waitlist countdown pattern as a general "stat row" component: bold `display-m` numerals, thin `1px` vertical dividers tinted with `--accent` at 20% opacity, `label` typography beneath. Good fit for dashboard KPIs (secret count, project count, active collaborators) — not just the marketing countdown.

### 7.8 Avatar stack
Overlapping circles (`-space-x-3`), 2px `--accent` ring, bold initials centered, optional `--success` status dot bottom-right. Direct reuse for the "who has access to this project" UI in the sharing flow (§1.4 of the architecture doc) — the same visual pattern that sells "teams are already joining" on the waitlist now shows real collaborators in the product.

### 7.9 The terminal chip (signature device)
`--bg-elevated` fill (dark mode) or `#0E0E0C` fill even in light mode (this one component stays dark-on-light deliberately, like a literal terminal window, for contrast and recognizability), `mono-m` type, `--radius-md`, small dot-triad or `$` prompt prefix, optional blinking cursor. Used in: hero sections, onboarding step-by-step, empty states ("No projects yet — run `$ envi init` to create one"), and docs code samples.

### 7.10 Tables (audit log, secret lists)
Hairline `--border` row dividers, no zebra striping (keep it calm — the production-tag left-border already does the "draw the eye" job where it matters). Row hover: `--bg-elevated` lightened 4%.

### 7.11 Empty states & errors
Per product voice (§8): state what happened and what to do, no apology tone. Visually: icon (outline, `--text-tertiary`) + `heading-m` + `body-m` + one primary action, generous whitespace, no illustration required. A terminal chip showing the exact command to run is often the best possible empty-state action for this product specifically (e.g. "No secrets yet" → `$ envi push`).

---

## 8. Voice & microcopy

- **Active voice, plain verbs, sentence case.** "Push rejected — your local version is behind" not "An error has occurred during the push operation."
- **Name things by what people control.** "Collaborators," not "access grant subjects." "Environments," not "deployment targets."
- **A button's label and its resulting confirmation match exactly.** Button says "Revoke access" → toast says "Access revoked," not "Done."
- **Errors state the fix, not just the problem.** "Push rejected — run `envi diff` to see what changed," not "Push failed."
- **No exclamation points in system copy**, including empty states — the tone is matter-of-fact, not enthusiastic. Save any warmth for genuinely celebratory moments (first successful `envi push`), used once, not habitually.

---

## 9. Accessibility floor

- All body text meets WCAG AA (4.5:1). Note: `--accent` yellow on `--bg` (dark mode) as *text* passes comfortably (~15:1) — but yellow text is never used on light-mode backgrounds (fails badly); light mode yellow is fill-only.
- Every interactive element has a visible focus ring — see §7.2 for the dark-ring-on-yellow-button exception.
- Minimum touch target 44×44px, even where the visual element (e.g. a small icon button) is smaller — pad the hit area.
- `prefers-reduced-motion` is respected everywhere per §6.
- Color is never the only signal — the production-environment tag (§7.5) pairs color with the word "Production," and status dots pair color with adjacent text, not color alone.

---

## 10. Preventing generic shadcn defaults (critical for AI-assisted building)

**The problem, precisely:** shadcn/ui components are copied into your codebase as real files, not imported from a themed package — which is exactly why they're good (fully editable, accessible by default), but also why "wire up my color tokens" isn't enough. Genericness hides in three separate places, and only the first is commonly fixed automatically:

1. **Global CSS variables** (`globals.css`, `tailwind.config`) — easy, most AI tools update this correctly when told to use a custom theme.
2. **Component-level hardcoded Tailwind classes** — each shadcn component file (`button.tsx`, `card.tsx`, etc.) has its own `rounded-md`, its own shadow class, its own padding, written directly into the JSX. These do **not** inherit from your tokens automatically — they have to be individually edited, component by component, or they silently keep shadcn's defaults even after your theme "changes."
3. **Layout habits** — shadcn's own docs/demo site established a specific visual grammar (boxed card grids, icon-over-title-over-description feature blocks, a particular density) that gets reproduced reflexively by AI tools regardless of brand, because it's the pattern they've seen thousands of times in training.

### 10.1 Explicit override table — hand this to whoever/whatever is building

| Component | shadcn default | Envi required |
|---|---|---|
| Button (primary) | `rounded-md` (6px), solid neutral bg, `shadow-sm` | `rounded-full`, `--accent` fill + black text, **no shadow**, hover = brightness shift + scale 1.02 (§7.2) |
| Card | `rounded-lg` (8px), `border` + `shadow-sm`, white/zinc-900 bg | `--radius-xl` (28px) hero / `--radius-lg` (20px) standard, **border + glow, no shadow in dark mode** (§4), warm near-black/off-white bg — never shadcn's zinc scale |
| Badge | `rounded-md`, small, muted secondary bg | `--radius-full`, filled `--accent` + black text for status badges (§7.4) — shadcn's muted-gray badge style is banned outright for anything brand-facing |
| Input | `rounded-md`, gray `border-input`, default focus ring | Two shapes by context (§7.3): pill for marketing, `--radius-md` for dashboard — both use the accent glow focus state, never shadcn's default gray ring |
| Table | mostly neutral, low-risk | Add the production-row left-border treatment (§7.5) — shadcn gives you a plain table, the brand-specific part has to be added deliberately, it won't appear on its own |
| Dialog/Modal | white/zinc card, `font-semibold text-lg` title | Structure is fine to keep; **typography must be remapped** — `DialogTitle` should use `heading-m`/Cabinet Grotesk, not shadcn's default title class |
| Toast (sonner) | plain white/gray | Recolor success/danger/warning states using §1.3's semantic tokens — default sonner is invisible-brand by design |
| Tabs | underline active state | Consider converting to the filled-pill active state (§7.1) to match the nav pattern — reusing one "how we show 'active'" idea consistently is a cheap way to feel less templated |

### 10.2 A standing instruction to give the AI every session

Worth pasting at the start of any frontend-building session, not assumed as implicit:

> Before writing any UI, read the full design system doc. When using a shadcn/ui component, treat its copied code as a bare structural skeleton only — every hardcoded Tailwind class that conflicts with the design system's tokens (radius, color, shadow, typography) must be explicitly edited, not just the global CSS variables. Do not leave any shadcn default gray/zinc/slate color classes in a component that's brand-facing. After building each component, compare it against the design system doc section by section and flag anywhere you kept a shadcn default, and why.

### 10.3 A gut-check habit, not a one-time fix

After any page gets built, look at it and ask literally: *"could this be shadcn.com's own demo site with the colors swapped?"* If yes, that's the signal to push back on a specific component, not the whole page — genericness is usually concentrated in 2–3 components (almost always Button, Card, and Badge), not everywhere at once.

**Where to spend the AI's default competence vs. where to fight it:** shadcn's defaults are genuinely fine, even good, for dense/functional dashboard surfaces — tables, settings forms, modals — where correctness and accessibility matter more than novelty, and where a user isn't looking for personality anyway. Reserve the real fight against genericness for high-visibility surfaces: the marketing site, the hero, onboarding, empty states, the terminal chip (§7.9). That's consistent with the "spend your boldness in one place" principle already guiding this system — don't burn effort making the settings page distinctive, spend it where the brand is actually being seen and judged.

---

## 11. Implementation stack

Recommended for building this system out — chosen specifically to be approachable for a non-frontend-first workflow with AI assistance, not just "what's popular":

| Layer | Choice | Reasoning |
|---|---|---|
| Framework | Next.js (App Router) | Already decided in the architecture plan |
| Styling | Tailwind CSS | Utility classes map directly onto §1–§4's tokens (colors, spacing, radius) as Tailwind theme config — one source of truth, not scattered CSS |
| Component primitives | shadcn/ui (built on Radix UI) | Copied into the codebase as editable code rather than an opaque installed package — comes with correct accessibility (keyboard nav, ARIA) by default, which matters most when no one is manually auditing it. Also the pattern most AI coding tools are best-trained on, which tends to produce cleaner output |
| Icons | Lucide React | Matches §5 |
| Animation | Motion (formerly Framer Motion) | Covers §6's hover/fade/cursor-blink behavior without hand-rolled CSS keyframes |
| Fonts | `next/font` | Handles Inter and JetBrains Mono directly (Google Fonts); Cabinet Grotesk needs self-hosting via Fontshare (free, ~2-minute setup, not on Google Fonts) |

**Token wiring:** define §1's colors and §3's spacing/radius as CSS custom properties in `globals.css` (`:root` for light, `.dark` for dark — matches Tailwind's `darkMode: 'class'` strategy and shadcn's existing theming convention), then reference them in `tailwind.config` rather than hardcoding hex values in components. This keeps the design doc and the actual code from drifting apart as the product grows — change a token once, it updates everywhere.

**Practical suggestion for reviewing progress as a non-frontend person:** consider a lightweight Storybook setup (or even just a single `/design-system` route in the app itself) that renders every component from §7 in isolation, both modes, so you can visually check "does this match the doc" without reading component code line by line.

---

## 12. Dark/light parity checklist

When building any new component, confirm both modes explicitly — don't assume inversion is correct:
- [ ] Does this use `--accent` as a fill (safe in both modes) or as text (dark-mode-only)?
- [ ] Does elevation use glow (dark) or shadow (light) appropriately, not both/neither?
- [ ] Does the terminal chip stay dark-on-light in light mode (§7.9), rather than inverting?
- [ ] Do environment tags (§7.5) still read clearly against the light-mode card background?