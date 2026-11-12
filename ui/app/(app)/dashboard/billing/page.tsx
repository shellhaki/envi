"use client";
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { AlertTriangle, Check, CreditCard, ExternalLink, RefreshCw, Users } from "lucide-react";
import Toast from "@/components/toast";

/*
  Billing: which plan you're on, what you're using, and how to change it.

  Nothing here changes a plan directly. Every upgrade sends you to a Bachs
  checkout, and the plan only changes when Bachs's webhook confirms the card or
  the payment. So after returning from checkout the page polls for a short
  while: the webhook usually lands within seconds, but not always before the
  redirect does.
*/

type Account = {
  plan: "free" | "payg" | "team"; currency: "USD" | "NGN";
  card_saved: boolean; card_brand: string; card_last4: string;
  subscription_status: string; cancel_at_period_end: boolean;
  current_period_end: string | null; payg_cap: number;
};
type Charge = {
  month: string; currency: string; members: number; amount_minor: number;
  carried_minor: number; status: string; attempts: number; last_error: string;
};
type Overview = {
  enabled: boolean; account: Account; people: { email: string }[];
  counted_this_month: number; projected_minor: number; past_due: boolean;
  charges: Charge[]; team_available: boolean;
  prices: { per_member: Record<string, number>; minimum: Record<string, number>; team: number };
};

const SYMBOL: Record<string, string> = { USD: "$", NGN: "₦" };
function money(minor: number, currency: string) {
  const digits = currency === "NGN" && minor % 100 === 0 ? 0 : 2;
  return `${SYMBOL[currency] ?? ""}${(minor / 100).toLocaleString("en", { minimumFractionDigits: digits, maximumFractionDigits: 2 })}`;
}
const PLAN_NAME = { free: "Free", payg: "Pay as you go", team: "Team" } as const;
const STATUS_LABEL: Record<string, string> = {
  carried: "Rolled into next month", processing: "Processing", succeeded: "Paid",
  failed: "Failed", settled: "Paid with a later month",
};

async function call(path: string, body?: object) {
  const response = await fetch(`/api/envi${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = response.status === 204 ? {} : await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error ?? "Something went wrong.");
  return data;
}

export default function BillingPage() {
  const params = useSearchParams();
  const [data, setData] = useState<Overview>();
  const [currency, setCurrency] = useState<"USD" | "NGN">("USD");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [cancelling, setCancelling] = useState(false);

  const load = useCallback(async () => {
    try {
      const overview: Overview = await call("/billing");
      setData(overview);
      if (overview.account.plan === "payg") setCurrency(overview.account.currency);
      return overview;
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  // A completed checkout lands on /billing/success; only an abandoned one comes
  // back here.
  useEffect(() => {
    if (params.get("checkout") === "cancelled") setNotice("Checkout cancelled. Nothing was changed.");
  }, [params]);

  async function act(name: string, path: string, body: object = {}) {
    setBusy(name);
    setError("");
    try {
      const result = await call(path, body);
      if (result.url) { window.location.href = result.url; return; }
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  }

  if (!data) {
    return <main className="product-page"><div className="page-title"><div><span className="eyebrow">Workspace</span><h1>Billing</h1></div></div>
      {error ? <p className="form-error">{error}</p> : <div className="panel billing-loading">Loading…</div>}</main>;
  }

  const { account: a, prices } = data;
  const teamOn = a.plan === "team" && ["active", "trialing", "past_due"].includes(a.subscription_status);
  const perPerson = prices.per_member[currency];
  const projectedUSD = a.plan === "payg" && a.currency === "USD" ? data.projected_minor : 0;
  const teamCheaper = projectedUSD > prices.team;

  return (
    <main className="product-page billing-page">
      <div className="page-title">
        <div>
          <span className="eyebrow">Workspace</span>
          <h1>Billing</h1>
          <p>Every plan has every feature. You only pay for the people you share with.</p>
        </div>
      </div>

      {!data.enabled && (
        <section className="panel billing-off">
          <Check />
          <div>
            <strong>This instance doesn&rsquo;t bill.</strong>
            <p>Self-hosted Envi has no plans and no limits. Invite as many people as you like.</p>
          </div>
        </section>
      )}

      {data.enabled && <>
        {data.past_due && (
          <div className="billing-alert">
            <AlertTriangle />
            <span>A pay-as-you-go charge failed. You can&rsquo;t add anyone until it&rsquo;s paid. Update your card, then retry.</span>
            <button className="button secondary" disabled={!!busy} onClick={() => act("portal", "/billing/portal")}>Update card</button>
            <button className="button primary" disabled={!!busy} onClick={() => act("retry", "/billing/retry")}>
              <RefreshCw />{busy === "retry" ? "Retrying…" : "Retry now"}
            </button>
          </div>
        )}

        <section className="panel billing-current">
          <div className="billing-current-main">
            <span className="billing-label">Current plan</span>
            <strong>{PLAN_NAME[a.plan]}</strong>
            <span className="admin-dim">
              {a.plan === "free" && "Just you. Add a plan to share with anyone else."}
              {a.plan === "payg" && `${money(prices.per_member[a.currency], a.currency)} per extra person per month, charged in ${a.currency}.`}
              {teamOn && (a.cancel_at_period_end
                ? `Ends ${a.current_period_end ? new Date(a.current_period_end).toLocaleDateString() : "at the end of this period"}.`
                : `${money(prices.team, "USD")} a month${a.current_period_end ? `, renews ${new Date(a.current_period_end).toLocaleDateString()}` : ""}.`)}
              {a.plan === "team" && !teamOn && `Subscription ${a.subscription_status || "pending"}.`}
            </span>
          </div>
          <div className="billing-current-side">
            {a.card_saved && (
              <span className="billing-card"><CreditCard />{a.card_brand || "Card"} {a.card_last4 && `•••• ${a.card_last4}`}</span>
            )}
            <div className="billing-actions">
              {a.card_saved && (
                <button className="button secondary" disabled={!!busy} onClick={() => act("portal", "/billing/portal")}>
                  <ExternalLink />Manage payment
                </button>
              )}
              {(a.plan === "payg" || (teamOn && !a.cancel_at_period_end)) && (
                <button className="button ghost danger" disabled={!!busy} onClick={() => setCancelling(true)}>
                  {a.plan === "team" ? "Cancel Team" : "Leave pay as you go"}
                </button>
              )}
            </div>
          </div>
        </section>

        <section className="billing-plans">
          <div className={"panel billing-plan" + (a.plan === "free" ? " current" : "")}>
            <span className="billing-label">Free</span>
            <strong className="billing-price">{money(0, "USD")}</strong>
            <p>Just you, with every feature. Projects are always unlimited.</p>
            {a.plan === "free" && <span className="badge">Current</span>}
          </div>

          <div className={"panel billing-plan" + (a.plan === "payg" ? " current" : "")}>
            <span className="billing-label">Pay as you go</span>
            <div className="billing-currency" role="group" aria-label="Currency">
              {(["USD", "NGN"] as const).map((c) => (
                <button key={c} className={currency === c ? "on" : ""} onClick={() => setCurrency(c)}>{c}</button>
              ))}
            </div>
            <strong className="billing-price">{money(perPerson, currency)}<small> / extra person / month</small></strong>
            <p>
              Charged once a month to a saved card, only for people you actually shared with.
              {prices.minimum[currency] > 0 && ` Months under ${money(prices.minimum[currency], currency)} roll into the next one.`}
            </p>
            {a.plan === "payg" && a.currency === currency ? <span className="badge">Current</span> : (
              <button className="button primary" disabled={!!busy || teamOn}
                      title={teamOn ? "Cancel Team first" : undefined}
                      onClick={() => act("payg", "/billing/payg", { currency })}>
                {busy === "payg" ? "Opening…" : a.plan === "payg" ? `Switch to ${currency}` : `Choose pay as you go (${currency})`}
              </button>
            )}
          </div>

          <div className={"panel billing-plan" + (teamOn ? " current" : "")}>
            <span className="billing-label">Team</span>
            <strong className="billing-price">{money(prices.team, "USD")}<small> / month, flat</small></strong>
            <p>Unlimited people for one price. Billed in USD.</p>
            {teamOn ? <span className="badge">Current</span> : (
              <button className="button primary" disabled={!!busy || !data.team_available}
                      title={data.team_available ? undefined : "Team isn't set up on this instance yet"}
                      onClick={() => act("team", "/billing/team")}>
                {busy === "team" ? "Opening…" : "Upgrade to Team"}
              </button>
            )}
          </div>
        </section>

        {teamCheaper && (
          <p className="billing-nudge">
            This month is heading for {money(projectedUSD, "USD")} on pay as you go. Team is {money(prices.team, "USD")} flat, so it would cost less.
          </p>
        )}

        <section className="panel">
          <div className="panel-head">
            <h2>Usage this month</h2>
            {a.plan === "payg" && <span className="badge">so far {money(data.projected_minor, a.currency)}</span>}
          </div>
          <div className="billing-usage">
            <div><Users /><strong>{data.people.length}</strong><span>extra {data.people.length === 1 ? "person" : "people"} right now</span></div>
            <div><Check /><strong>{data.counted_this_month}</strong><span>counted this month</span></div>
            {a.plan === "payg" && <div><strong>{data.people.length}/{a.payg_cap}</strong><span>of the pay-as-you-go cap</span></div>}
          </div>
          {data.people.length > 0 && (
            <ul className="billing-people">{data.people.map((p) => <li key={p.email}><code>{p.email}</code></li>)}</ul>
          )}
          <p className="admin-foot">
            Anyone you share a project with counts once a month, however many projects they&rsquo;re on.
            Removing someone stops them counting next month, not this one.
          </p>
        </section>

        {data.charges.length > 0 && (
          <section className="panel">
            <div className="panel-head"><h2>Charges</h2></div>
            <div className="data-table billing-charges">
              <div className="thead"><span>Month</span><span>People</span><span>Amount</span><span>Status</span></div>
              {data.charges.map((c) => (
                <div className="trow" key={c.month}>
                  <span>{new Date(c.month + "-01T00:00:00Z").toLocaleDateString("en", { month: "long", year: "numeric", timeZone: "UTC" })}</span>
                  <span>{c.members}</span>
                  <span>
                    {money(c.amount_minor + c.carried_minor, c.currency)}
                    {c.carried_minor > 0 && <small className="admin-dim"> incl. {money(c.carried_minor, c.currency)} carried</small>}
                  </span>
                  <span className={"billing-status " + c.status} title={c.last_error || undefined}>{STATUS_LABEL[c.status] ?? c.status}</span>
                </div>
              ))}
            </div>
          </section>
        )}
      </>}

      {cancelling && (
        <CancelDialog
          plan={a.plan === "team" ? "team" : "payg"}
          endsOn={a.current_period_end}
          close={() => setCancelling(false)}
          onConfirm={async () => {
            await call("/billing/cancel", {});
            setCancelling(false);
            await load();
            setNotice(a.plan === "team"
              ? `Team is cancelled. It stays active until ${a.current_period_end ? new Date(a.current_period_end).toLocaleDateString() : "the end of this period"}.`
              : "You've left pay as you go. This month so far is still charged; nobody loses access.");
          }}
        />
      )}
      {error && <Toast key={error} kind="error" message={error} onClose={() => setError("")} />}
      {notice && <Toast key={notice} kind="notice" message={notice} onClose={() => setNotice("")} />}
    </main>
  );
}

/*
  The cancel confirmation, in the app's own dialog rather than the browser's.

  No typing-to-confirm, unlike deleting a project: cancelling destroys nothing
  and can be undone by choosing the plan again. It still says plainly what
  happens, because "will I lose access?" is the question every person asks at
  this button.
*/
function CancelDialog({ plan, endsOn, close, onConfirm }: {
  plan: "team" | "payg"; endsOn: string | null; close: () => void; onConfirm: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !busy) close(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [busy, close]);

  const team = plan === "team";
  return (
    <div className="dialog-backdrop" onMouseDown={busy ? undefined : close}>
      <form className="dialog" onMouseDown={(e) => e.stopPropagation()} onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        setError("");
        try { await onConfirm(); } catch (x) { setBusy(false); setError((x as Error).message); }
      }}>
        <header><h2>{team ? "Cancel Team?" : "Leave pay as you go?"}</h2><button type="button" onClick={close} disabled={busy}>×</button></header>
        <p className="dialog-sub">
          {team
            ? `Team stays active until ${endsOn ? new Date(endsOn).toLocaleDateString() : "the end of the period you've paid for"}, then you move to Free.`
            : "This month so far is still charged at the start of next month, for the people who had access."}
          {" "}Nobody loses access. You just can&rsquo;t add anyone new until you pick a plan again.
        </p>
        {error && <p className="form-error">{error}</p>}
        <div className="dialog-actions">
          <button type="button" className="button secondary" onClick={close} disabled={busy}>{team ? "Keep Team" : "Stay on pay as you go"}</button>
          <button className="button danger" disabled={busy}>
            {busy && <span className="spinner" />}{busy ? "Cancelling…" : team ? "Cancel Team" : "Leave pay as you go"}
          </button>
        </div>
      </form>
    </div>
  );
}
