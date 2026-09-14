import { describe, expect, it } from "vitest";
import { fromHex, importKey, open, seal, toHex } from "../src/crypto";

const KEY = "01234567890123456789012345678901";

// Fixtures produced by the Go server's internal/crypto (cmd/xcompat).
// If this file ever needs regenerating because the format changed, that is the
// signal that existing secrets just became unreadable.
import fixtures from "./fixtures/go-sealed.json";

const goSealed: Record<string, string> = fixtures;

describe("ciphertext compatibility with the Go server", () => {
  it("opens everything Go sealed", async () => {
    const key = await importKey(KEY);
    for (const [plain, hex] of Object.entries(goSealed)) {
      expect(await open(key, fromHex(hex))).toBe(plain);
    }
  });

  it("uses the same layout: nonce(12) then ciphertext and tag(16)", async () => {
    const key = await importKey(KEY);
    const sealed = await seal(key, "hello");
    expect(sealed.byteLength).toBe(12 + "hello".length + 16);
  });

  it("uses a fresh nonce per seal", async () => {
    const key = await importKey(KEY);
    const a = toHex(await seal(key, "same"));
    const b = toHex(await seal(key, "same"));
    expect(a).not.toBe(b);
  });

  it("round-trips its own output", async () => {
    const key = await importKey(KEY);
    for (const plain of Object.keys(goSealed)) {
      expect(await open(key, await seal(key, plain))).toBe(plain);
    }
  });

  it("rejects a wrong key", async () => {
    const wrong = await importKey("abcdefghijabcdefghijabcdefghijab");
    const hex = Object.values(goSealed)[0]!;
    await expect(open(wrong, fromHex(hex))).rejects.toThrow();
  });

  it("rejects tampered ciphertext", async () => {
    const key = await importKey(KEY);
    const sealed = await seal(key, "tamper me");
    sealed[sealed.byteLength - 1] = (sealed[sealed.byteLength - 1] ?? 0) ^ 0xff;
    await expect(open(key, sealed)).rejects.toThrow();
  });

  it("requires a 32-byte key", async () => {
    await expect(importKey("too-short")).rejects.toThrow();
  });
});
