"use client";
import { useState } from "react";
import { Check, Clipboard } from "lucide-react";

// A shell command with a copy button.
export default function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch { /* the command is on screen to select by hand */ }
  }
  return <div className="copy-command">
    <code><span aria-hidden="true">$ </span>{command}</code>
    <button type="button" className="icon-btn" title={copied ? "Copied" : "Copy"} aria-label="Copy command" onClick={copy}>{copied ? <Check /> : <Clipboard />}</button>
  </div>;
}
