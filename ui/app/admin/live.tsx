"use client";
import { useEffect, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { RefreshCw } from "lucide-react";

/*
  Keeps the operator screen current.

  Polling rather than a socket: the page is one server-rendered snapshot taken
  in a single database transaction, and re-taking it is both cheap and exactly
  what makes every number on screen agree with every other. A stream would push
  thirteen counters that each arrived at a different moment.

  It pauses while the tab is hidden. An admin screen left open on a second
  monitor overnight should not spend the night querying.
*/

const EVERY = 15_000;

export default function Live({ generatedAt }: { generatedAt: string }) {
  const router = useRouter();
  const [pending, start] = useTransition();
  const [paused, setPaused] = useState(false);
  const [ago, setAgo] = useState("just now");

  useEffect(() => {
    const onVisibility = () => setPaused(document.hidden);
    onVisibility();
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  }, []);

  useEffect(() => {
    if (paused) return;
    const id = setInterval(() => start(() => router.refresh()), EVERY);
    return () => clearInterval(id);
  }, [paused, router]);

  // The label is its own timer so the age keeps counting between refreshes
  // rather than freezing at "just now" until the next one lands.
  useEffect(() => {
    const tick = () => {
      const seconds = Math.max(0, Math.round((Date.now() - new Date(generatedAt).getTime()) / 1000));
      setAgo(seconds < 10 ? "just now" : seconds < 60 ? `${seconds}s ago` : `${Math.round(seconds / 60)}m ago`);
    };
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [generatedAt]);

  return (
    <div className="admin-live">
      <span className={"admin-live-dot" + (paused ? " paused" : "")} />
      <span>{paused ? "Paused" : "Live"}</span>
      <span className="admin-dim">· {ago}</span>
      <button className="button ghost" onClick={() => start(() => router.refresh())} disabled={pending}>
        <RefreshCw />{pending ? "Refreshing" : "Refresh"}
      </button>
    </div>
  );
}
