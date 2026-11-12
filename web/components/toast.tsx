"use client";
import { useEffect, useRef, useState } from "react";
import { CircleAlert, CircleCheck, X } from "lucide-react";

// Errors stay up longer: they usually need reading, not just noticing.
const LIFETIME = { notice: 4500, error: 8000 };
const EXIT_MS = 200;

/**
 * A small notification that rises in from the bottom, like the cookie notice
 * but lighter, and leaves on its own. Hovering holds it so it can be read.
 * Key it by message so a new message replays the entrance.
 */
export default function Toast({ kind, message, onClose }: { kind: "notice" | "error"; message: string; onClose: () => void }) {
  const [leaving, setLeaving] = useState(false);
  const [held, setHeld] = useState(false);
  // The parent passes a fresh closure every render; reading it through a ref
  // keeps the timers below from restarting on each of those renders.
  const close = useRef(onClose);
  useEffect(() => { close.current = onClose; });

  useEffect(() => {
    if (!leaving) return;
    const id = setTimeout(() => close.current(), EXIT_MS);
    return () => clearTimeout(id);
  }, [leaving]);

  useEffect(() => {
    if (held || leaving) return;
    const id = setTimeout(() => setLeaving(true), LIFETIME[kind]);
    return () => clearTimeout(id);
  }, [held, leaving, kind]);

  const Icon = kind === "error" ? CircleAlert : CircleCheck;
  return (
    <div className={`toast ${kind}${leaving ? " leaving" : ""}`} role={kind === "error" ? "alert" : "status"}
      onMouseEnter={() => setHeld(true)} onMouseLeave={() => setHeld(false)}>
      <Icon className="toast-icon" aria-hidden="true" />
      <p>{message}</p>
      <button type="button" className="toast-close" aria-label="Dismiss" onClick={() => setLeaving(true)}><X /></button>
    </div>
  );
}
