import type { Message } from "../mail";
import { escapeHtml, hostOf } from "./html";

// The sender name ("Haki from Envi") comes from RESEND_FROM.
export function welcomeEmail(to: string, webUrl: string, docsUrl: string): Message {
  const web = (webUrl.trim() || "https://envisecrets.com").replace(/\/+$/, "");
  const docs = (docsUrl.trim() || "https://docs.envisecrets.com").replace(/\/+$/, "");
  return {
    to,
    subject: "You're home — a note from Haki at Envi",
    text: text(web, docs),
    html: html(web, docs),
    headers: { "List-Unsubscribe": `<mailto:unsubscribe@${hostOf(web)}?subject=unsubscribe>` },
  };
}

function text(web: string, docs: string) {
  return `Hey,

You're home.

I'm Haki. I built Envi, and I wanted to say thanks for signing up.

Envi keeps your environment secrets encrypted, scoped to the people who
should see them, and out of the places they usually end up: a chat
thread, someone's downloads folder, a commit nobody noticed.

The idea is that your secrets never sit on anyone's laptop as a file.
You pull them into the process that needs them, for exactly as long as
it runs, and nothing is left behind afterwards.

Your dashboard is ready whenever you are:
${web}

The docs walk through the first few minutes:
${docs}

If something is broken or confusing, just reply. It comes to me.

I hope you stay.

Haki
Envi
`;
}

// Inline styles, because email clients drop <style> blocks.
function html(web: string, docs: string) {
  const bg = "#fafafa", surface = "#ffffff", line = "#e8e8ea", ink = "#09090b";
  const body = "#3f3f46", muted = "#71717a", faint = "#a1a1aa", accent = "#f5e73f";
  const font = "-apple-system,BlinkMacSystemFont,'Segoe UI',Inter,Roboto,Helvetica,Arial,sans-serif";
  return `<!doctype html>
<html><body style="margin:0;padding:32px 16px;background:${bg};font-family:${font};color:${ink};-webkit-font-smoothing:antialiased;">
<div style="max-width:560px;margin:0 auto;background:${surface};border:1px solid ${line};border-radius:16px;overflow:hidden;">

  <div style="height:4px;background:${accent};"></div>

  <div style="padding:36px 36px 40px;">
    <p style="margin:0 0 28px;font-size:11px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:${faint};">Envi</p>

    <h1 style="margin:0 0 20px;font-size:26px;line-height:1.25;font-weight:600;letter-spacing:-.02em;color:${ink};">You&rsquo;re home.</h1>

    <p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:${body};">
      I&rsquo;m Haki. I built Envi, and I wanted to say thanks for signing up.
      This is a real address &mdash; replying to it reaches me.
    </p>

    <p style="margin:0 0 16px;font-size:15px;line-height:1.65;color:${body};">
      Envi keeps your environment secrets encrypted, scoped to the people who should see
      them, and out of the places they usually end up: a chat thread, someone&rsquo;s
      downloads folder, a commit nobody noticed.
    </p>

    <p style="margin:0 0 28px;font-size:15px;line-height:1.65;color:${body};">
      The idea is that your secrets never sit on anyone&rsquo;s laptop as a file. You pull
      them into the process that needs them, for exactly as long as it runs, and nothing
      is left behind afterwards.
    </p>

    <a href="${escapeHtml(web)}" style="display:inline-block;padding:12px 22px;background:${ink};color:${surface};text-decoration:none;border-radius:9px;font-size:14px;font-weight:600;letter-spacing:-.01em;">Open your dashboard</a>

    <p style="margin:28px 0 0;font-size:15px;line-height:1.65;color:${body};">
      The <a href="${escapeHtml(docs)}" style="color:${muted};">documentation</a> walks through the first few minutes.
    </p>

    <p style="margin:24px 0 0;font-size:15px;line-height:1.65;color:${body};">
      I hope you stay.<br>
      <span style="color:${muted};">Haki</span>
    </p>
  </div>

  <div style="padding:18px 36px;border-top:1px solid ${line};background:${bg};">
    <p style="margin:0;font-size:12px;line-height:1.6;color:${faint};">
      You received this once, because you created an Envi account. No list, no newsletter.
    </p>
  </div>
</div></body></html>`;
}
