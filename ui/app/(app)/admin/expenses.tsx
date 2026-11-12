"use client";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";
import { Plus, Trash2 } from "lucide-react";

/*
  What the instance costs, and a form to add to it.

  Amounts are handled as whole minor units — kobo, cents — from the moment they
  leave the input. The typed string is parsed to an integer here and an integer
  is what goes over the wire, so no float is ever rounded on the way to the
  database.
*/

type Expense = {
  id: string; amount_minor: number; currency: string; category: string;
  description: string; spent_on: string; recurring: string;
};
type CurrencyTotal = { currency: string; total_minor: number; monthly_minor: number; count: number };
type CategoryTotal = { category: string; currency: string; total_minor: number; count: number };

const SYMBOL: Record<string, string> = { NGN: "₦", USD: "$" };

export function money(minor: number, currency: string) {
  return `${SYMBOL[currency] ?? ""}${(minor / 100).toLocaleString("en", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

/**
 * toMinor turns what somebody typed into whole minor units.
 *
 * It works on the decimal string rather than on a parsed float: Number("20.07")
 * * 100 is 2006.9999999999998, and Math.round papers over that only until an
 * amount comes along where it does not.
 */
function toMinor(input: string): number | null {
  const cleaned = input.replace(/[\s,]/g, "");
  if (!/^\d+(\.\d{0,2})?$/.test(cleaned)) return null;
  const [whole, fraction = ""] = cleaned.split(".");
  return Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
}

export default function Expenses({
  totals, categories, recent, defaultCurrency,
}: {
  totals: CurrencyTotal[]; categories: CategoryTotal[];
  recent: Expense[]; defaultCurrency: string;
}) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const amount = toMinor(String(form.get("amount") ?? ""));
    if (amount === null || amount <= 0) {
      setError("Amount must be a number, with at most two decimal places.");
      return;
    }
    setBusy(true);
    setError("");
    const response = await fetch("/api/envi/admin/expenses", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        amount_minor: amount,
        currency: form.get("currency"),
        category: form.get("category"),
        description: form.get("description"),
        spent_on: form.get("spent_on") || undefined,
        recurring: form.get("recurring"),
      }),
    }).catch(() => null);
    setBusy(false);

    if (!response?.ok) {
      const body = await response?.json().catch(() => ({}));
      setError(body?.error ?? "Could not save that.");
      return;
    }
    setOpen(false);
    router.refresh();
  }

  async function remove(id: string, label: string) {
    if (!confirm(`Delete the ${label} expense?`)) return;
    await fetch(`/api/envi/admin/expenses/${id}`, { method: "DELETE" }).catch(() => null);
    router.refresh();
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>What it costs</h2>
        <button className="button primary" onClick={() => setOpen((v) => !v)}>
          <Plus />{open ? "Cancel" : "Add expense"}
        </button>
      </div>

      {open && (
        <form className="admin-expense-form" onSubmit={add}>
          <label>
            <span>Amount</span>
            <input name="amount" inputMode="decimal" placeholder="20.00" required autoFocus />
          </label>
          <label>
            <span>Currency</span>
            <select name="currency" defaultValue={defaultCurrency}>
              <option value="NGN">NGN ₦</option>
              <option value="USD">USD $</option>
            </select>
          </label>
          <label>
            <span>Category</span>
            <input name="category" placeholder="vps" required list="expense-categories" />
            <datalist id="expense-categories">
              {["vps", "domain", "email", "database", "storage", "tooling", "marketing"].map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
          </label>
          <label>
            <span>Repeats</span>
            <select name="recurring" defaultValue="monthly">
              <option value="once">Once</option>
              <option value="monthly">Monthly</option>
              <option value="yearly">Yearly</option>
            </select>
          </label>
          <label>
            <span>Date</span>
            <input name="spent_on" type="date" defaultValue={new Date().toISOString().slice(0, 10)} />
          </label>
          <label className="admin-expense-wide">
            <span>Note</span>
            <input name="description" placeholder="Hetzner CX22, Falkenstein" />
          </label>
          {error && <p className="form-error admin-expense-wide">{error}</p>}
          <button className="button primary admin-expense-wide" disabled={busy}>
            {busy ? "Saving…" : "Save expense"}
          </button>
        </form>
      )}

      {totals.length ? (
        <div className="admin-spend-totals">
          {totals.map((t) => (
            <div key={t.currency} className="admin-spend-total">
              <span>Running cost</span>
              <strong>{money(t.monthly_minor, t.currency)}<small>/month</small></strong>
              {/* Totals are never added across currencies: there is no
                  exchange rate anywhere in this product, and inventing one
                  would make every figure on this page a guess. */}
              <small>{money(t.total_minor, t.currency)} all time, {t.count} item{t.count === 1 ? "" : "s"}</small>
            </div>
          ))}
        </div>
      ) : (
        <p className="admin-foot">Nothing recorded yet. Add the VPS, the domain, the email provider.</p>
      )}

      {categories.length > 0 && (
        <div className="admin-spend-cats">
          {categories.map((c) => (
            <span key={`${c.category}-${c.currency}`} className="badge">
              {c.category} {money(c.total_minor, c.currency)}
            </span>
          ))}
        </div>
      )}

      {recent.length > 0 && (
        <div className="data-table admin-expense-table">
          <div className="thead"><span>Category</span><span>Amount</span><span>Repeats</span><span>Date</span><span /></div>
          {recent.map((e) => (
            <div className="trow" key={e.id}>
              <span>
                <strong>{e.category}</strong>
                {e.description && <small className="admin-dim"> {e.description}</small>}
              </span>
              <span className="admin-amount">{money(e.amount_minor, e.currency)}</span>
              <span className={e.recurring === "once" ? "admin-dim" : ""}>{e.recurring}</span>
              <span className="admin-dim">{e.spent_on.slice(0, 10)}</span>
              <button className="icon-btn danger" title="Delete"
                      onClick={() => remove(e.id, `${e.category} ${money(e.amount_minor, e.currency)}`)}>
                <Trash2 />
              </button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
