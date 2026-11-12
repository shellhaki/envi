// The client for Bachs (https://docs.bachs.io), Envi's payment provider.

import { equalHex, hmacSha256Hex } from "../crypto";

export const SANDBOX_URL = "https://sandbox-api.bachs.io";
export const LIVE_URL = "https://api.bachs.io";
export const SIGNATURE_TOLERANCE_SECONDS = 5 * 60;

export type Checkout = { checkout_id: string; checkout_url: string };
export type Charge = { id: string; status: string };
export type BachsEvent = { id: string; type: string; created_at?: string; data: any };

// Bachs is the part of the provider billing uses; tests replace it with a fake.
export interface Bachs {
  live: boolean;
  createCustomer(email: string, name: string, metadata: Record<string, string>): Promise<string>;
  saveCardCheckout(customerId: string, successUrl: string, cancelUrl: string, metadata: Record<string, string>): Promise<Checkout>;
  subscriptionCheckout(customerId: string, productId: string, successUrl: string, cancelUrl: string, metadata: Record<string, string>): Promise<Checkout>;
  cancelSubscription(subscriptionId: string): Promise<void>;
  chargeSavedCard(customerId: string, minor: number, currency: string, description: string, key: string): Promise<Charge>;
  portalUrl(customerId: string): Promise<string>;
}

// BachsError is a non-2xx reply from Bachs. Its body is logged, never shown to users.
export class BachsError extends Error {
  constructor(public status: number, public body: string) {
    super(`bachs: ${status}: ${body}`);
  }
}

// bachsClient picks sandbox or live from the key itself.
export function bachsClient(rawKey: string): Bachs {
  const key = rawKey.trim();
  let base: string;
  if (key.startsWith("sk_sandbox_")) base = SANDBOX_URL;
  else if (key.startsWith("sk_live_")) base = LIVE_URL;
  else if (key === "") throw new Error("no Bachs key");
  else throw new Error("unrecognised Bachs key: expected sk_sandbox_ or sk_live_");

  async function call(method: string, path: string, body?: unknown, idempotencyKey?: string): Promise<any> {
    const headers: Record<string, string> = { Authorization: `Bearer ${key}` };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
    const res = await fetch(base + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(20_000),
    });
    const text = (await res.text()).slice(0, 1 << 20);
    if (res.status >= 300) throw new BachsError(res.status, text.trim());
    return text ? JSON.parse(text) : {};
  }

  return {
    live: base === LIVE_URL,
    async createCustomer(email, name, metadata) {
      const out = await call("POST", "/v1/customers", { email, name, metadata });
      const id = out.customer_id || out.id;
      if (!id) throw new Error("bachs: customer created with no id in the reply");
      return id;
    },
    saveCardCheckout(customerId, successUrl, cancelUrl, metadata) {
      return call("POST", "/v1/checkout-sessions", {
        customer: { customer_id: customerId },
        save_payment_method: true,
        success_url: successUrl,
        cancel_url: cancelUrl,
        metadata,
      });
    },
    subscriptionCheckout(customerId, productId, successUrl, cancelUrl, metadata) {
      return call("POST", "/v1/checkout-sessions", {
        product_cart: [{ product_id: productId, quantity: 1 }],
        customer: { customer_id: customerId },
        billing_currency: "USD",
        success_url: successUrl,
        cancel_url: cancelUrl,
        metadata,
      });
    },
    async cancelSubscription(subscriptionId) {
      await call("DELETE", `/v1/subscriptions/${subscriptionId}`, {
        cancel_at_period_end: true,
        reason: "Cancelled from the Envi dashboard",
      });
    },
    async chargeSavedCard(customerId, minor, currency, description, key) {
      const out = await call(
        "POST", "/v1/charges",
        { customer: customerId, amount: money(minor), currency, description, reference: key },
        key,
      );
      return { id: out.charge_id || out.id || "", status: out.status || "" };
    },
    async portalUrl(customerId) {
      return (await call("POST", `/v1/customers/${customerId}/portal-sessions`, {})).url;
    },
  };
}

// money writes minor units as the decimal string Bachs wants: 1234 -> "12.34".
export function money(minor: number): string {
  const sign = minor < 0 ? "-" : "";
  const abs = Math.abs(minor);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
}

// verifyWebhook checks a delivery's HMAC signature and age, then decodes it.
export async function verifyWebhook(secret: string, headers: Headers, body: string, now: Date): Promise<BachsEvent> {
  if (!secret) throw new Error("bachs: no webhook signing secret configured");

  let { timestamp, signatures } = parseSignatureHeader(headers.get("X-Bachs-Signature-V2") ?? "");
  if (!timestamp) {
    timestamp = headers.get("X-Bachs-Timestamp") ?? "";
    const v1 = headers.get("X-Bachs-Signature");
    if (v1) signatures.push(v1);
  }
  const bad = new Error("bachs: webhook signature does not verify");
  if (!timestamp || signatures.length === 0 || !/^-?\d+$/.test(timestamp)) throw bad;
  const ageSeconds = now.getTime() / 1000 - Number(timestamp);
  if (Math.abs(ageSeconds) > SIGNATURE_TOLERANCE_SECONDS) throw bad;

  const expected = await hmacSha256Hex(secret, `${timestamp}.${body}`);
  if (!signatures.some((s) => equalHex(s.trim().toLowerCase(), expected))) throw bad;

  const event = JSON.parse(body) as BachsEvent;
  if (!event.id || !event.type) throw new Error("bachs: webhook has no id or type");
  return event;
}

// signWebhook produces a V2 signature header the way Bachs does, for tests.
export async function signWebhook(secret: string, body: string, at: Date): Promise<string> {
  const timestamp = String(Math.floor(at.getTime() / 1000));
  return `t=${timestamp},v1=${await hmacSha256Hex(secret, `${timestamp}.${body}`)}`;
}

// parseSignatureHeader reads "t=1700000000,v1=abc,v1=def".
function parseSignatureHeader(value: string) {
  let timestamp = "";
  const signatures: string[] = [];
  for (const part of value.split(",")) {
    const [key, val] = part.trim().split("=", 2);
    if (val === undefined) continue;
    if (key === "t") timestamp = val;
    if (key === "v1") signatures.push(val);
  }
  return { timestamp, signatures };
}
