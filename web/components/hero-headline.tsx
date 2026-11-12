"use client";
import { useEffect, useState } from "react";

// How long each line stays up before the next one swipes in.
const HOLD_MS = 2600;

const LINES: React.ReactNode[] = [
  <>The home for your team&apos;s secrets.</>,
  <>No more <mark>.env</mark> files passed around.</>,
  <>Fits the workflow you already have.</>,
  <>Production stays production.</>,
  <>Every change, on the record.</>,
];

/**
 * The landing headline. It cycles through LINES, each one swiping up and
 * fading out as the next arrives. The two lines on screen share one grid
 * cell; a min-height in CSS holds the heading's size as lines change.
 */
export default function HeroHeadline() {
  // Counts swaps; the line shown and the one leaving are derived from it.
  const [tick, setTick] = useState(0);
  // The tick whose outgoing line has finished its exit, so it can be dropped.
  const [exited, setExited] = useState(-1);
  const index = tick % LINES.length;
  const prev = tick > 0 ? (tick - 1) % LINES.length : null;

  useEffect(() => {
    // Motion is what's being opted out of: leave the first line up.
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const id = setInterval(() => setTick((t) => t + 1), HOLD_MS);
    return () => clearInterval(id);
  }, []);

  return (
    // Screen readers get the first line once, not a heading that keeps changing.
    <h1 className="lp-rotator" aria-label="The home for your team's secrets.">
      {/* Only the current line and the one leaving are in the DOM, so a copy,
          a reader view or missing CSS shows one headline, not all five. */}
      {prev !== null && exited !== tick && <span key={prev} className="off" aria-hidden="true" onAnimationEnd={() => setExited(tick)}>{LINES[prev]}</span>}
      <span key={index} className="on" aria-hidden="true">{LINES[index]}</span>
    </h1>
  );
}
