// Secrets are AES-256-GCM, stored as nonce + ciphertext + tag: the same bytes the Go server wrote.

const NONCE_BYTES = 12;

export type Cipher = {
  seal(plain: string): Promise<Buffer>;
  open(sealed: Uint8Array): Promise<string>;
};

export async function makeCipher(key: string): Promise<Cipher> {
  const raw = new TextEncoder().encode(key);
  if (raw.length !== 32) throw new Error("encryption key must be 32 bytes");
  const aesKey = await crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]);
  return {
    async seal(plain) {
      const nonce = crypto.getRandomValues(new Uint8Array(NONCE_BYTES));
      const sealed = await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, aesKey, new TextEncoder().encode(plain));
      return Buffer.concat([nonce, new Uint8Array(sealed)]);
    },
    async open(stored) {
      const sealed = new Uint8Array(stored);
      if (sealed.length < NONCE_BYTES) throw new Error("invalid ciphertext");
      const plain = await crypto.subtle.decrypt(
        { name: "AES-GCM", iv: sealed.subarray(0, NONCE_BYTES) },
        aesKey,
        sealed.subarray(NONCE_BYTES),
      );
      return new TextDecoder().decode(plain);
    },
  };
}

// randomToken is 32 random bytes as 64 hex characters.
export function randomToken(): string {
  return toHex(crypto.getRandomValues(new Uint8Array(32)));
}

// hashToken is what is stored instead of a token, so a database leak reveals no usable tokens.
export async function hashToken(token: string): Promise<Buffer> {
  return Buffer.from(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(token)));
}

export async function sha256Hex(value: string): Promise<string> {
  return toHex(new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value))));
}

export async function hmacSha256Hex(secret: string, message: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return toHex(new Uint8Array(await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(message))));
}

// equalHex compares in constant time, so it leaks nothing about how much matched.
export function equalHex(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

export function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// randomBelow returns a uniform random integer in [0, n).
export function randomBelow(n: number): number {
  const limit = Math.floor(0x1_0000_0000 / n) * n;
  for (;;) {
    const value = crypto.getRandomValues(new Uint32Array(1))[0];
    if (value < limit) return value % n;
  }
}
