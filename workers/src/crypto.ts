/**
 * Wire-compatible with internal/crypto in the Go server:
 * AES-256-GCM, no AAD, layout `nonce(12) ‖ ciphertext ‖ tag(16)`.
 *
 * Both servers read the same database, so this layout is not an
 * implementation detail — changing it makes existing secrets unreadable.
 */

const NONCE_BYTES = 12;

export async function importKey(key: string): Promise<CryptoKey> {
  const raw = new TextEncoder().encode(key);
  if (raw.byteLength !== 32) throw new Error("ENVI_ENCRYPTION_KEY must be exactly 32 bytes");
  return crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]);
}

export async function seal(key: CryptoKey, plain: string): Promise<Uint8Array> {
  const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES));
  const sealed = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: nonce },
    key,
    new TextEncoder().encode(plain),
  );
  const out = new Uint8Array(NONCE_BYTES + sealed.byteLength);
  out.set(nonce, 0);
  out.set(new Uint8Array(sealed), NONCE_BYTES);
  return out;
}

export async function open(key: CryptoKey, data: Uint8Array): Promise<string> {
  if (data.byteLength <= NONCE_BYTES) throw new Error("invalid ciphertext");
  const plain = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: data.subarray(0, NONCE_BYTES) },
    key,
    data.subarray(NONCE_BYTES),
  );
  return new TextDecoder().decode(plain);
}

/** Tokens are stored as SHA-256 hashes; the plaintext exists only at issuance. */
export async function hashToken(token: string): Promise<Uint8Array> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(token));
  return new Uint8Array(digest);
}

export function randomToken(bytes = 32): string {
  return toHex(crypto.getRandomValues(new Uint8Array(bytes)));
}

export function toHex(bytes: Uint8Array): string {
  return [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export function fromHex(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}
