// Email login codes, sent and checked through Redis with a rate limit.

import { equalHex, randomBelow, sha256Hex } from "../crypto";
import type { Deps } from "../deps";
import type { Message } from "../mail";
import { RateLimited } from "../errors";
import { findOrCreateAccount } from "./workspace";

const CODE_SECONDS = 10 * 60;
const MAX_GUESSES = 10;
const CODES_PER_HOUR = 20;

// sendLoginCode emails a fresh 6-digit code and stores only its hash.
export async function sendLoginCode(deps: Deps, rawEmail: string): Promise<void> {
  const email = normalise(rawEmail);
  const requests = await deps.redis.incr(`otp:req:${email}`, 60 * 60);
  if (requests > CODES_PER_HOUR) throw new RateLimited("too many OTP requests");

  const code = String(randomBelow(1_000_000)).padStart(6, "0");
  await deps.redis.set(`otp:${email}`, await sha256Hex(code), CODE_SECONDS);
  await deps.mailer.send(codeEmail(rawEmail.trim(), code));
}

// checkLoginCode verifies the code and returns the user ID, creating the account on first login.
// A wrong guess leaves the code usable; a right one uses it up.
export async function checkLoginCode(deps: Deps, rawEmail: string, code: string): Promise<string> {
  const email = normalise(rawEmail);
  const guesses = await deps.redis.incr(`otp:try:${email}`, CODE_SECONDS);
  if (guesses > MAX_GUESSES) throw new Error("too many attempts");

  const stored = await deps.redis.get(`otp:${email}`);
  if (!stored || !equalHex(stored, await sha256Hex(code.trim()))) throw new Error("invalid or expired OTP");
  await deps.redis.del(`otp:${email}`);
  return findOrCreateAccount(deps, email);
}

function normalise(email: string) {
  return email.trim().toLowerCase();
}

function codeEmail(to: string, code: string): Message {
  return {
    to,
    subject: "Envi login code",
    text: `Your Envi code is ${code}. It expires soon.`,
    html: `<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
<h1 style="margin:0 0 16px;font-size:20px;">Your login code</h1>
<p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">Enter this code to sign in. It expires in a few minutes.</p>
<div style="font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:32px;font-weight:700;letter-spacing:.14em;background:#f2f4f7;border-radius:8px;padding:16px 24px;text-align:center;">${code}</div>
<p style="margin:24px 0 0;color:#98a2b3;font-size:12px;line-height:1.5;">If you didn't request this, you can safely ignore this email — no one can sign in without the code above.</p>
</div></body></html>`,
  };
}
