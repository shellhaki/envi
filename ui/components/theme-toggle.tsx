"use client";
import { Moon, Sun } from "lucide-react";
export default function ThemeToggle() {
  function toggle() { const next = document.documentElement.dataset.theme !== "dark"; document.documentElement.dataset.theme = next ? "dark" : "light"; localStorage.setItem("envi_theme", next ? "dark" : "light"); }
  return <button className="icon-button theme-toggle" onClick={toggle} aria-label="Toggle color mode"><Moon className="moon" /><Sun className="sun" /></button>;
}
