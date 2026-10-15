# Billing

Built against the Bachs sandbox: `internal/bachs` (client and webhook
verification), `internal/billing` (plans, counting, the monthly charge job,
webhooks), `migrations/011_billing.sql`, and the billing page at
`/dashboard/billing`. Not yet run against live keys.

## Where this starts

There is no billing code in the repository today: no payment provider, no
subscriptions, no plans, no seats. `beta_testers.json` holds six addresses,
three of which are the founder's. So the first decision is not which tier
structure to use — it is that nothing can currently accept money at all.

Everything below is sized for that: the smallest thing that can take a payment,
with a clear path to the more elaborate version if the first one earns it.

The payment provider is [Bachs](https://docs.bachs.io/). What it does and does
not support today shapes several decisions below, and each place that depends
on it says so.

## Pricing

**Free for one person. Pay a little per extra person, or a flat price for the
team. No feature gates.**

| Plan | Price | Members | Projects | Features |
|---|---|---|---|---|
| Free | $0 | 1 | Unlimited | All of them |
| Pay as you go | $0.40 per extra member per month | Up to a cap | Unlimited | All of them |
| Team | $4 / month, flat | Unlimited | Unlimited | All of them |

- **Free** is the founder of an organization working alone. Inviting anyone
  else needs one of the two paid options.
- **Pay as you go** is not a subscription. Each extra member costs $0.40 for
  every month they are in the organization, and the month's total is charged
  once, at the start of the following month, to a saved card.
- **Team** is a monthly subscription at a flat $4 for the whole organization,
  however many members it has.

Pay as you go is cheaper up to ten extra members. From ten upward Team costs
the same or less, and the dashboard should say so at the moment it becomes
true.

### Why per-member rather than per-project

A project costs nothing to run. It is a row in `projects` and some rows in
`secrets`. Metering it charges for something that is free to provide, and it
punishes the exact behaviour that makes the product sticky — trying Envi on one
more repository.

Members are where both the value and the cost actually are. A person sharing
secrets with three colleagues has the problem Envi solves. A person with
fourteen side projects and no colleagues does not.

For the same reason, pay as you go counts people in the **organization**, not
people per project. A colleague on three projects is one extra member and
$0.40, not $1.20.

### Why so few plans

Every plan costs a quota constant, a Bachs product, a row in a comparison
table, and a moment of hesitation from a buyer who cannot tell two of them
apart. Three options that differ only in how you pay — nothing, a little per
person, or a flat rate — need no comparison table at all.

It also fits the positioning. The argument against Infisical is that it has
become a platform with feature gates — dynamic secrets on Pro, KMS and approval
workflows on Enterprise. "Every plan has everything, no sales call" is a
sharper weapon than being cheaper, and it is one a competitor with feature
tiers structurally cannot copy.

### Why not the word "Enterprise"

It sets expectations that cannot currently be met: SSO, SCIM, an SLA, a signed
DPA, invoicing on net-30, a security questionnaire answered without
embarrassment. Envi today has one encryption key across all tenants and an
`access.Allow` rule that lets org owners reach production without a grant. A
plan called Enterprise invites exactly the customer who will find both.

If a top plan is wanted later — Team at a flat $4 undercharges a forty-person
organization — call it **Business** and let it mean volume, not features.

### Why this shape fits Bachs

Bachs does not yet support per-seat or quantity-based subscriptions — its docs
list them as coming soon, and today a plan is a fixed-price product — and it
has no metered billing. Both halves of this pricing avoid needing either:

- **Team** is a fixed-price recurring product, which Bachs supports directly,
  along with renewal, failed-payment retries, the card-update email and a
  hosted customer portal.
- **Pay as you go** is a one-off charge Envi computes each month and collects
  against a saved card off-session. Envi owns the counting and the charge;
  Bachs only moves the money.

The cost is that pay as you go is real work on Envi's side: counting members
per month, a monthly charge job, and handling a charge that fails. It is
specified below.

## Pay as you go, specified

### What is counted

An extra member is any member of the organization beyond the first. A member
counts for a month if they were in the organization at any point during that
calendar month (UTC), once, however many projects they are on.

Counting "at any point" rather than "at month end" means adding someone for a
week and removing them before the 1st still costs $0.40. That is the honest
charge, and it closes the obvious way to game it.

### When and how it is charged

- At the start of each month a job totals the previous month for every
  organization on pay as you go: extra members × $0.40.
- The total is charged once to the organization's saved card through Bachs,
  off-session, with an idempotency key of the organization and month so a
  retried job cannot charge twice.
- A month with no extra members charges nothing.

**Small charges.** A card payment of $0.40 or $1.20 may lose most of itself to
a fixed per-transaction fee. Check the fixed fee on
[Bachs fees](https://docs.bachs.io/for-you/fees). If it is significant, carry
amounts below a minimum (say $2) forward to the next month instead of charging
them.

### Getting a card on file

The first time an organization on Free invites a second member, the invitation
is refused until a card is saved — through Bachs checkout, following the
[Charge a saved card](https://docs.bachs.io/guides/payments/charge-a-saved-card)
guide. Whether a card can be saved there without an initial charge needs
confirming; if it cannot, the first charge can be the first month's $0.40 in
advance.

### Cap

A cap on extra members, configurable per organization, so a runaway invitation
script cannot produce a surprise bill. Default: 20 extra members. Invitations
beyond the cap are refused, with a pointer to Team.

### When the charge fails

Bachs's automatic payment recovery applies to subscriptions, not to these
one-off charges, so Envi handles it:

- Retry after 3 days, then after 7.
- Email the organization owner after each failure, with a link to update the
  card.
- After the last failure, refuse new invitations until the balance is paid.
  Nothing is removed and nobody loses access.

### Switching between pay as you go and Team

- **Pay as you go → Team:** charge the current month's pay-as-you-go total so
  far, then start the Team subscription through checkout.
- **Team → pay as you go:** cancel Team at period end. Pay as you go starts
  counting from the day Team ends.

## Where enforcement goes

Three places, all of which already exist and already take the organization or a
path to it:

| Check | Enforced in |
|---|---|
| Free: no second member | `invitation.Service.Create`, and again on accept |
| Pay as you go: card on file, under the cap, not past due | `invitation.Service.Create`, and again on accept |
| Same checks | wherever a membership is created directly, if any path does |

Projects are never limited, so `project.Service.Create` needs no check.

Checking on both invitation and acceptance matters: an organization one member
below its cap can otherwise send ten invitations and land eleven members.

Accepting an invitation is also where the member is recorded as billable for
the current month.

The check belongs in the domain packages rather than in HTTP middleware,
because the CLI and the dashboard reach these through the same service calls
and neither should be able to route around it.

### What a refusal looks like

`402 Payment Required`, with a code the CLI can act on rather than a string it
has to parse:

```json
{ "code": "payment_required", "error": "the free plan is for one member; add a card for pay as you go or upgrade to Team", "plan": "free" }
```

Other codes: `payg_cap_reached`, `payg_past_due`.

`internal/cli` already maps API errors to exit codes in `ExitCode`; this needs a
case for 402 so a script can tell "you need to pay" from "you are not allowed".

### Downgrades

Never delete anything. An organization that drops back to Free keeps every
member and every project, and is refused only on the *next* addition.
Destroying a collaborator's access because a card failed is not a behaviour
worth having.

## Self-hosting

**Billing has to be inert unless explicitly switched on.** The repository is
public and the whole stack is self-hostable; a quota check that fires by
default would mean shipping a paywall into other people's infrastructure.

Billing is on only when `envi.config.json` says `"mode": "hosted"` **and**
`BACHS_SECRET_KEY` holds a valid key. A missing or unreadable config file means
self-hosted. Hosted with no key logs a warning and runs with billing off rather
than crashing, since a crash would take sign-in down too. When billing is off,
every check allows, the charge job never starts, and no Bachs client exists.

A live key (`sk_live_`) on an instance with no `ENVIRONMENT` set refuses to
start: a local machine must never charge real cards.

This also wants saying plainly in the self-hosting docs: running your own
instance has no limits and no licence.

## Bachs objects needed

Bachs conventions that apply throughout: money is a decimal string with an ISO
4217 currency (`"4.00"`, `"USD"`), never minor units; ids carry prefixes
(`cust_`, `prod_`, `sub_`, `chk_`, `evt_`); and there is no create-subscription
endpoint — a subscription exists only once a customer completes a checkout for
a recurring product.

**Currency.** Bachs subscriptions currently bill USD cards only. NGN
subscriptions exist only on accounts where Bachs has enabled them. Prices in
this document are in USD on that basis.

**Shared**

- **Customer** per organization, created the first time it pays for anything,
  id stored against the organization.
- **Customer portal** (`POST /v1/customers/{id}/portal-sessions`) for card
  updates, cancellation and invoice history, so none of it needs UI in Envi.

**Team**

- One **Product**, "Envi Team", recurring `billing_cycle`
  `{ "interval": "month", "frequency": 1 }`, price `"4.00"` USD.
- **Checkout session** (`POST /v1/checkout-sessions`) to start it.
- **Webhooks**: `customer.subscription.created`, `.updated`, `.deleted`,
  `invoice.paid`, `invoice.payment_failed`. Each one writes `plan`, `status`
  and `current_period_end` for the organization.

**Pay as you go**

- A saved card on the customer, from checkout (see *Getting a card on file*).
- A monthly off-session charge against it for the month's total, with an
  `Idempotency-Key` of `payg:{org_id}:{month}`.
- **Webhooks**: `collection.succeeded` and `collection.failed` update that
  month's charge.

### Webhook correctness

The webhook is the only place payment state changes. Rules:

- **Verify the signature.** Use `X-Bachs-Signature-V2` (`t={timestamp},v1={sig}`):
  compute HMAC-SHA256 over `"{timestamp}.{raw_body}"` with the endpoint's
  signing secret and accept the delivery if any `v1=` value matches, so secret
  rotation does not break verification. Reject timestamps older than five
  minutes. Read the raw body before any JSON decoding. An unverified billing
  webhook is an endpoint that lets anyone grant themselves a paid plan.
- **Be idempotent on the event `id`.** Bachs delivers at least once. Insert the
  id in the same transaction as the state change; an id already recorded
  means the event has already been applied.
- **Ignore stale events.** Delivery order is not guaranteed. Skip an event
  whose `created_at` is older than the last event applied to that
  organization.
- **Decode leniently.** Bachs adds fields without notice, so do not use
  `DisallowUnknownFields` on the payload.
- **Respond correctly.** `400` only for a bad signature; `5xx` if Envi's own
  write fails, so Bachs retries. Respond within 10 seconds.

## Rollout order

1. Database migration, defaults only. Nothing reads it.
2. `BillingEnabled` flag, off. Plan checks and the monthly totalling written and
   unit-tested, still never called.
3. Enforcement wired into `invitation.Create` and acceptance, and billable
   members recorded on accept, behind the flag. Hosted deployment
   still has it off.
4. Bachs: customer, Team checkout, card saving, portal, webhook and the monthly
   charge job, built against the sandbox (`sandbox-api.bachs.io`,
   `sk_sandbox_` keys). Then the Bachs account goes through verification, the
   keys are swapped for `sk_live_`, and the founder pays for both plans with a
   real card.
5. Lift the beta gate in `beta_testers.json`.
6. Turn `ENVI_BILLING=on` in production.

Steps 1 to 3 are safe to ship any time and change nothing for anyone.

## Before any of this

One thing is worth more than the whole plan: **there are three external users
and none of them has been asked for money.** A price they accept or refuse is
better evidence than any amount of pricing design, and that conversation needs
no code at all.

Also outstanding, and relevant because the first paying team will include
someone who reads the documentation: `access.Allow` lets organization owners
and admins reach production without a grant, while
`docs/app/concepts/access-and-permissions` states that organization membership
alone grants no ambient access to production. One of the two has to change.

## Open questions

- ~~What counts as a member?~~ **Settled: grant holders.** Accepting an
  invitation creates an `access_grant`, never a `membership` — every
  organization is a personal one whose only member is its owner. Counting
  memberships would find nobody and bill nothing. An extra person is somebody
  holding a grant on one of the organization's projects who is not one of its
  members, counted once however many projects they are on.
- ~~Exactly $0.40?~~ **Settled.** Bachs charges USD cards 5% + $0.40 per
  charge, so a $0.40 month costs more to collect than it brings in. USD months
  under **$3** roll into the next month (about 82% kept after fees). Local
  NGN cards cost a flat 2% with no fixed fee, so naira months — **₦600** per
  extra person — are charged every month. The customer chooses the currency
  on the billing page. Prices and minimums are constants in
  `internal/billing/billing.go`. **₦600 is a placeholder to confirm.**
- **Cap pay as you go at the Team price?** "You never pay more than $4" is
  generous and simple to explain, but it makes Team pointless for anyone on
  pay as you go. The alternative is the nudge described above: tell the owner
  when Team becomes cheaper.
- **Trial on Team?** A 14-day trial costs little and removes the decision from
  the moment of discovery. It also adds a `trialing` path through every check.
  Bachs trials are currently in beta.
- **Annual Team?** Two months free on annual is standard and improves cash
  position considerably at this size. On Bachs it is one more product, since a
  product's billing interval is fixed once created.
- **What happens when Team is `past_due`?** Bachs retries the charge and emails
  the customer a card-update link. When retries run out it moves to `canceled`
  (the default) or `unpaid`, depending on an account setting. Recommended:
  nothing is lost or locked during `past_due`; at `canceled` or `unpaid` the
  organization reverts to Free with all data intact.