import { isLocalUrl } from "../config";
import type { Message } from "../mail";
import { escapeHtml, hostOf } from "./html";

export function announcementEmail(to: string, subject: string, body: string, webUrl: string): Message {
  const web = webUrl.trim() || "https://envisecrets.com";
  return {
    to,
    subject,
    text: `${body}\n\n—\nEnvi\n${web}\n\nReply to this email to reach us, or reply with "unsubscribe" to stop receiving these.\n`,
    html: html(subject, body, web),
    headers: { "List-Unsubscribe": `<mailto:unsubscribe@${hostOf(web) || "envisecrets.com"}?subject=unsubscribe>` },
  };
}

// bannerUrl is the image at the top of every announcement. A local dashboard uses the public copy.
function bannerUrl(web: string) {
  const base = isLocalUrl(web) ? "https://envisecrets.com" : web;
  return base.replace(/\/+$/, "") + "/email/newsletter-banner.png";
}

// html escapes what the operator typed and keeps their paragraph breaks.
function html(subject: string, body: string, web: string) {
  const paragraphs = body
    .replaceAll("\r\n", "\n")
    .split("\n\n")
    .map((block) => block.trim())
    .filter(Boolean)
    .map((block) => `<p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:#3f3f46;">${escapeHtml(block).replaceAll("\n", "<br>")}</p>`);

  return `<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:#fafafa;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Inter,Roboto,Helvetica,Arial,sans-serif;color:#09090b;-webkit-font-smoothing:antialiased;">
<div style="max-width:560px;margin:0 auto;background:#ffffff;border:1px solid #e8e8ea;border-radius:16px;overflow:hidden;">
  <img src="${escapeHtml(bannerUrl(web))}" width="560" alt="Envi" style="display:block;width:100%;max-width:560px;height:auto;border:0;background:#09090b;">
  <div style="padding:36px 36px 40px;">
    <h1 style="margin:0 0 20px;font-size:24px;line-height:1.3;font-weight:600;letter-spacing:-.02em;color:#09090b;">${escapeHtml(subject)}</h1>
    ${paragraphs.join("\n    ")}
  </div>
  <div style="padding:18px 36px;border-top:1px solid #e8e8ea;background:#fafafa;">
    <p style="margin:0;font-size:12px;line-height:1.6;color:#a1a1aa;">
      You receive these because you have an Envi account at
      <a href="${escapeHtml(web)}" style="color:#71717a;text-decoration:none;">${hostOf(web) || "envisecrets.com"}</a>.
      Reply with &ldquo;unsubscribe&rdquo; to stop.
    </p>
  </div>
</div></body></html>`;
}
