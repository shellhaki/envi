import type { Env } from "./types";

type Message = { to: string; subject: string; text: string; html?: string };

export async function send(env: Env, msg: Message): Promise<void> {
  const res = await fetch("https://api.resend.com/emails", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${env.RESEND_API_KEY}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      from: env.RESEND_FROM,
      to: [msg.to],
      subject: msg.subject,
      text: msg.text,
      ...(msg.html ? { html: msg.html } : {}),
    }),
  });
  if (!res.ok) {
    throw new Error(`Resend API error (${res.status}): ${await res.text()}`);
  }
}

export function otpEmail(code: string): Message {
  return {
    to: "",
    subject: `${code} is your Envi sign-in code`,
    text: `Your Envi sign-in code is ${code}. It expires in 10 minutes.\n\nIf you didn't request it, ignore this email.`,
    html: shell(`
      <h1 style="margin:0 0 16px;font-size:20px;">Your sign-in code</h1>
      <p style="margin:0 0 20px;color:#475467;font-size:14px;line-height:1.6;">Enter this code to finish signing in. It expires in 10 minutes.</p>
      <p style="margin:0;font-size:30px;font-weight:700;letter-spacing:.18em;">${code}</p>
      <p style="margin:24px 0 0;color:#98a2b3;font-size:12px;">If you didn't request this, you can ignore it.</p>
    `),
  };
}

export function invitationEmail(projectName: string, permission: string, link: string, expires: string): Message {
  return {
    to: "",
    subject: `You're invited to ${projectName} on Envi`,
    text:
      `You've been invited to collaborate on ${projectName} on Envi, with ${permission} access.\n\n` +
      `Accept your invitation: ${link}\n\n` +
      `If you don't have an Envi account yet, this link will let you create one with ${permission} access already waiting.\n\n` +
      `This invitation expires on ${expires}.`,
    html: shell(`
      <h1 style="margin:0 0 16px;font-size:20px;">You&rsquo;re invited to ${projectName}</h1>
      <p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">You&rsquo;ve been invited to collaborate on <strong>${projectName}</strong> with <strong>${permission}</strong> access.</p>
      <a href="${link}" style="display:inline-block;background:#0c111d;color:#fff;text-decoration:none;font-size:14px;font-weight:600;padding:12px 22px;border-radius:8px;">Accept invitation</a>
      <p style="margin:24px 0 0;color:#667085;font-size:13px;line-height:1.6;">Don&rsquo;t have an Envi account yet? This link creates one for you, with access already waiting.</p>
      <p style="margin:16px 0 0;color:#98a2b3;font-size:12px;line-height:1.6;">This invitation expires on ${expires}. If the button doesn&rsquo;t work, copy this link:<br><span style="word-break:break-all;">${link}</span></p>
    `),
  };
}

function shell(inner: string): string {
  return `<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#fff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
${inner}
</div></body></html>`;
}
