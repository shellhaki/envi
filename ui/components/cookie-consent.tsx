"use client";
import { useEffect, useState } from "react";
import { Cookie } from "lucide-react";

const KEY = "envi_cookie_consent";

export default function CookieConsent() {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    // Deferred a frame so the state update happens outside the effect's own
    // synchronous pass, rather than as a same-tick setState-during-render.
    const id = requestAnimationFrame(() => {
      try {
        if (!localStorage.getItem(KEY)) setVisible(true);
      } catch {
        // Private browsing or a blocked store — err toward showing the notice
        // rather than silently skipping it.
        setVisible(true);
      }
    });
    return () => cancelAnimationFrame(id);
  }, []);
  function accept() {
    try { localStorage.setItem(KEY, "1"); } catch {}
    setVisible(false);
  }
  if (!visible) return null;
  return (
    <div className="cookie-consent" role="dialog" aria-label="Cookie notice">
      <Cookie className="cookie-consent-icon" aria-hidden="true" />
      <p>We use one essential cookie to keep you signed in. No tracking, no analytics, no third parties.</p>
      <button className="button primary" onClick={accept}>Got it</button>
    </div>
  );
}
