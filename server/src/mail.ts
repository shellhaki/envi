// Message is one email. Fill in both text and html; text-only mail is more likely to be marked as spam.
export type Message = {
  to: string;
  subject: string;
  text: string;
  html: string;
  headers?: Record<string, string>;
};

export interface Mailer {
  send(message: Message): Promise<void>;
  // sendMany sends one message per recipient and returns how many failed.
  sendMany(messages: Message[]): Promise<number>;
}

// resendMailer sends through the Resend API. A single send tries up to three times.
export function resendMailer(apiKey: string, from: string): Mailer {
  const post = async (path: string, body: unknown) => {
    if (!apiKey) throw new Error("RESEND_API_KEY is required");
    if (!from) throw new Error("RESEND_FROM is required");
    let res: Response;
    try {
      res = await fetch(`https://api.resend.com${path}`, {
        method: "POST",
        headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
    } catch (err) {
      throw new Error(`connect to Resend: ${(err as Error).message}`);
    }
    if (res.status >= 300) throw new Error(`Resend API error (${res.status}): ${(await res.text()).slice(0, 65536)}`);
  };
  const toResend = (m: Message) => ({
    from,
    to: [m.to],
    subject: m.subject,
    text: m.text,
    html: m.html,
    ...(m.headers && Object.keys(m.headers).length > 0 ? { headers: m.headers } : {}),
  });

  return {
    async send(m) {
      let lastError: unknown;
      for (const wait of [0, 1000, 3000]) {
        await sleep(wait);
        try {
          return await post("/emails", toResend(m));
        } catch (err) {
          lastError = err;
        }
      }
      throw lastError;
    },
    async sendMany(messages) {
      let failed = 0;
      for (let i = 0; i < messages.length; i += 100) {
        const chunk = messages.slice(i, i + 100);
        try {
          await post("/emails/batch", chunk.map(toResend));
        } catch (err) {
          console.error(`batch send failed: ${(err as Error).message}`);
          failed += chunk.length;
        }
      }
      return failed;
    },
  };
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
