"use client";
import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, LayoutDashboard, CreditCard } from "lucide-react";
import Brand from "@/components/brand";

/*
  Where Bachs sends somebody after a successful checkout.

  It celebrates, but it does not claim more than it knows. The payment is
  Bachs's to confirm and arrives by webhook, sometimes after this page loads,
  so the plan line asks the API and says "activating" until the webhook has
  landed.

  It works signed out too: the redirect can arrive in a browser without a
  session, and a payment page that answers with a sign-in wall reads as if the
  payment failed.
*/

type Plan = "free" | "payg" | "team";
const PLAN_NAME: Record<Plan, string> = { free: "Free", payg: "Pay as you go", team: "Team" };

export default function BillingSuccess() {
  const [plan, setPlan] = useState<Plan | "unknown" | "pending">("pending");

  // Ask until the webhook has moved the plan off Free, for about twenty
  // seconds. Signed out is "unknown", and the page still makes sense.
  useEffect(() => {
    let tries = 0;
    let stopped = false;
    async function check() {
      tries++;
      const response = await fetch("/api/envi/billing").catch(() => null);
      if (stopped) return;
      if (!response || response.status === 401 || response.status === 403) { setPlan("unknown"); return; }
      const data = await response.json().catch(() => null);
      const current: Plan | undefined = data?.account?.plan;
      if (current && current !== "free") { setPlan(current); return; }
      if (tries < 10) setTimeout(check, 2000); else setPlan("unknown");
    }
    check();
    return () => { stopped = true; };
  }, []);

  return (
    <main className="success-page">
      <Ribbons />
      <section className="success-card">
        <div className="success-brand"><Brand /></div>
        <span className="success-mark"><Check /></span>
        <h1>You&rsquo;re all set.</h1>
        <p className="success-lede">
          Thanks for backing Envi. Your secrets stay where they belong, and now so does everyone you share them with.
        </p>
        <p className="success-plan" aria-live="polite">
          {plan === "pending" && <>Activating your plan&hellip;</>}
          {plan === "unknown" && <>Your plan activates as soon as the payment is confirmed. It usually takes a few seconds.</>}
          {(plan === "payg" || plan === "team" || plan === "free") && <>You&rsquo;re on <strong>{PLAN_NAME[plan]}</strong>.</>}
        </p>
        <div className="success-actions">
          <Link className="button primary" href="/dashboard"><LayoutDashboard />Go to dashboard</Link>
          <Link className="button secondary" href="/dashboard/billing"><CreditCard />Back to billing</Link>
        </div>
      </section>
    </main>
  );
}

/*
  The ribbon shower: a burst of thin twisting strips that falls once and stops.

  One canvas, drawn by hand rather than a confetti library, so the page ships
  nothing extra. Ribbons are spawned for the first couple of seconds and then
  allowed to fall out of frame, after which the loop ends — an animation that
  keeps running on a page someone has left open is a battery drain for nothing.

  With reduced motion set it draws nothing at all. The page still reads as a
  success without it.
*/
function Ribbons() {
  const canvas = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const el = canvas.current;
    if (!el || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const ctx = el.getContext("2d");
    if (!ctx) return;

    const dark = document.documentElement.dataset.theme === "dark";
    // The brand yellow carries it; the neutrals keep it from looking like a
    // party supply catalogue.
    const colors = dark
      ? ["#f5e73f", "#f5e73f", "#fafafa", "#a1a1aa", "#e2d014"]
      : ["#f5e73f", "#f5e73f", "#09090b", "#a1a1aa", "#e2d014"];

    let width = 0, height = 0;
    const resize = () => {
      const ratio = window.devicePixelRatio || 1;
      width = el.clientWidth; height = el.clientHeight;
      el.width = width * ratio; el.height = height * ratio;
      ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    };
    resize();
    window.addEventListener("resize", resize);

    type Ribbon = { x: number; y: number; vx: number; vy: number; w: number; h: number; angle: number; spin: number; twist: number; twistSpeed: number; sway: number; color: string };
    const ribbons: Ribbon[] = [];
    const spawn = (count: number) => {
      for (let i = 0; i < count; i++) {
        ribbons.push({
          x: Math.random() * width,
          y: -20 - Math.random() * height * 0.3,
          vx: (Math.random() - 0.5) * 1.2,
          vy: 1.6 + Math.random() * 2.4,
          w: 5 + Math.random() * 4,
          h: 12 + Math.random() * 16,
          angle: Math.random() * Math.PI * 2,
          spin: (Math.random() - 0.5) * 0.08,
          twist: Math.random() * Math.PI * 2,
          twistSpeed: 0.08 + Math.random() * 0.12,
          sway: Math.random() * Math.PI * 2,
          color: colors[(Math.random() * colors.length) | 0],
        });
      }
    };

    const start = performance.now();
    let frame = 0;
    const tick = (now: number) => {
      const elapsed = now - start;
      if (elapsed < 2200) spawn(elapsed < 400 ? 9 : 3);

      ctx.clearRect(0, 0, width, height);
      for (let i = ribbons.length - 1; i >= 0; i--) {
        const r = ribbons[i];
        r.sway += 0.03;
        r.x += r.vx + Math.sin(r.sway) * 0.6;
        r.y += r.vy;
        r.angle += r.spin;
        r.twist += r.twistSpeed;
        if (r.y > height + 40) { ribbons.splice(i, 1); continue; }

        // The twist is a horizontal squash: as cos passes through zero the
        // strip is seen edge-on, which is what makes it read as a ribbon
        // turning over rather than a falling square.
        ctx.save();
        ctx.translate(r.x, r.y);
        ctx.rotate(r.angle);
        ctx.scale(Math.cos(r.twist), 1);
        ctx.fillStyle = r.color;
        ctx.fillRect(-r.w / 2, -r.h / 2, r.w, r.h);
        ctx.restore();
      }

      if (elapsed < 2200 || ribbons.length > 0) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);

    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("resize", resize);
    };
  }, []);

  return <canvas ref={canvas} className="success-ribbons" aria-hidden="true" />;
}
