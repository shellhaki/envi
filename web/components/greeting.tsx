"use client";
import { useEffect, useState, useSyncExternalStore } from "react";

type Period = "morning" | "afternoon" | "evening";

// [language, morning, afternoon, evening]. English leads so the first thing
// anyone reads is plain; the rest rotate in after it.
const GREETINGS: [string, string, string, string][] = [
  ["English", "Good morning", "Good afternoon", "Good evening"],
  ["Yorùbá", "Ẹ káàárọ̀", "Ẹ káàsán", "Ẹ kú ìrọ̀lẹ́"],
  ["Igbo", "Ụtụtụ ọma", "Ehihie ọma", "Mgbede ọma"],
  ["Hausa", "Barka da safiya", "Barka da rana", "Barka da yamma"],
  ["Edo", "Ob'owie", "Ob'avan", "Ob'ota"],
  ["Français", "Bonjour", "Bon après-midi", "Bonsoir"],
  ["Kiswahili", "Habari za asubuhi", "Habari za mchana", "Habari za jioni"],
  ["Twi", "Maakye", "Maaha", "Maadwo"],
  ["Español", "Buenos días", "Buenas tardes", "Buenas noches"],
  ["Português", "Bom dia", "Boa tarde", "Boa noite"],
  ["Deutsch", "Guten Morgen", "Guten Tag", "Guten Abend"],
  ["日本語", "おはようございます", "こんにちは", "こんばんは"],
];
const COLUMN: Record<Period, 1 | 2 | 3> = { morning: 1, afternoon: 2, evening: 3 };
const HOLD_MS = 3200;

function period(hour: number): Period {
  if (hour >= 5 && hour < 12) return "morning";
  if (hour >= 12 && hour < 17) return "afternoon";
  return "evening";
}

// The hour comes from the visitor's clock, which the server can't know. The
// server snapshot is null, so the first paint holds the space and the real
// greeting arrives on hydration without a mismatch.
function subscribe(onChange: () => void) {
  const id = setInterval(onChange, 60_000);
  return () => clearInterval(id);
}
const clientHour = () => new Date().getHours();
const serverHour = () => null;

/**
 * Time-of-day greeting that cycles through languages, each swiping up as the
 * next arrives. Each line is keyed by its index, so a new line mounts fresh
 * and plays its entrance animation. The small caption says which language
 * and what it means.
 */
export default function Greeting({ name }: { name: string }) {
  const hour = useSyncExternalStore(subscribe, clientHour, serverHour);
  const [tick, setTick] = useState(0);
  // The tick whose outgoing line has finished its exit, so it can be dropped.
  const [exited, setExited] = useState(-1);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const id = setInterval(() => setTick((t) => t + 1), HOLD_MS);
    return () => clearInterval(id);
  }, []);

  const p = hour === null ? "morning" : period(hour);
  const col = COLUMN[p];
  const index = tick % GREETINGS.length;
  const prev = tick > 0 ? (tick - 1) % GREETINGS.length : null;
  const [language] = GREETINGS[index];
  const english = GREETINGS[0][col];

  return (
    <div className="greeting" style={hour === null ? { visibility: "hidden" } : undefined}>
      <h1 aria-label={`${english}, ${name}`}>
        {/* Only the current line and the one leaving are in the DOM, so a
            copy, a reader view or missing CSS shows one greeting, not all. */}
        {prev !== null && exited !== tick && <span key={prev} className="off" aria-hidden="true" onAnimationEnd={() => setExited(tick)}>{GREETINGS[prev][col]}, <b>{name}</b></span>}
        <span key={index} className="on" aria-hidden="true">{GREETINGS[index][col]}, <b>{name}</b></span>
      </h1>
      <p aria-hidden="true"><span key={index} className="greeting-lang">{language}</span>{index > 0 && <> · {english}</>}</p>
    </div>
  );
}
