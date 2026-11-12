import type { Message } from "../mail";
import type { Invitation } from "../services/invitations";

export function invitationEmail(invitation: Invitation, projectName: string, webUrl: string): Message {
  const link = webUrl.replace(/\/+$/, "") + "/invite/" + invitation.Token;
  const expiry = formatExpiry(invitation.ExpiresAt);
  const permission = invitation.Permission;

  const text =
    `You've been invited to collaborate on ${projectName} on Envi, with ${permission} access.\n\n` +
    `Accept your invitation: ${link}\n\n` +
    `If you don't have an Envi account yet, this link will let you create one with ${permission} access already waiting.\n\n` +
    `This invitation expires on ${expiry}.`;

  const html = `<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#f6f7f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#0c111d;">
<div style="max-width:480px;margin:0 auto;background:#ffffff;border:1px solid #e4e7ec;border-radius:12px;padding:32px;">
<p style="margin:0 0 8px;font-size:12px;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:#667085;">Envi</p>
<h1 style="margin:0 0 16px;font-size:20px;">You&rsquo;re invited to ${projectName}</h1>
<p style="margin:0 0 24px;color:#475467;font-size:14px;line-height:1.6;">You&rsquo;ve been invited to collaborate on <strong>${projectName}</strong> with <strong>${permission}</strong> access.</p>
<a href="${link}" style="display:inline-block;background:#0c111d;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:12px 22px;border-radius:8px;">Accept invitation</a>
<p style="margin:24px 0 0;color:#667085;font-size:13px;line-height:1.6;">Don&rsquo;t have an Envi account yet? This link creates one for you, with access already waiting.</p>
<p style="margin:16px 0 0;color:#98a2b3;font-size:12px;line-height:1.6;">This invitation expires on ${expiry}. If the button doesn&rsquo;t work, copy this link:<br><span style="word-break:break-all;">${link}</span></p>
</div></body></html>`;

  return { to: invitation.Email, subject: `You're invited to ${projectName} on Envi`, text, html };
}

// formatExpiry writes a date like "Jan 2, 2006 at 3:04 PM UTC".
function formatExpiry(date: Date): string {
  const day = date.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });
  const time = date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit", timeZone: "UTC" });
  return `${day} at ${time} UTC`;
}
