"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";

export type SelectOption = { value: string; label: string };

/**
 * A styled replacement for <select>. The native element renders an OS-drawn
 * menu that ignores CSS entirely, so the menu here is ours.
 *
 * `name` renders a hidden input so this still submits inside a plain <form>
 * read through FormData, exactly like the native element it replaces.
 */
export default function Select({
  options, value, onChange, name, placeholder = "Select…", disabled, ariaLabel,
}: {
  options: SelectOption[];
  value?: string;
  onChange?: (value: string) => void;
  name?: string;
  placeholder?: string;
  disabled?: boolean;
  ariaLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);

  const selected = options.find((o) => o.value === value);
  const close = useCallback(() => setOpen(false), []);
  // Seed the highlight when opening rather than syncing it from an effect —
  // an effect would set state during render-commit for no added benefit.
  const openMenu = useCallback(() => {
    setActive(Math.max(0, options.findIndex((o) => o.value === value)));
    setOpen(true);
  }, [options, value]);

  useEffect(() => {
    if (!open) return;
    // Pointer down rather than click: a click that starts inside the menu and
    // ends outside shouldn't count as "clicked away".
    function onPointerDown(e: PointerEvent) {
      if (!root.current?.contains(e.target as Node)) close();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") { e.stopPropagation(); close(); }
    }
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, close]);

  // Keep the highlighted option in view when arrowing through a long list.
  useEffect(() => {
    if (!open) return;
    menu.current?.querySelectorAll<HTMLElement>(".select-option")[active]?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  function pick(option: SelectOption) {
    onChange?.(option.value);
    close();
  }

  function onTriggerKey(e: React.KeyboardEvent) {
    if (["ArrowDown", "ArrowUp", "Enter", " "].includes(e.key)) {
      e.preventDefault();
      if (!open) { openMenu(); return; }
      if (e.key === "Enter" || e.key === " ") { const o = options[active]; if (o) pick(o); return; }
      setActive((i) => {
        const next = e.key === "ArrowDown" ? i + 1 : i - 1;
        return (next + options.length) % options.length;
      });
    }
  }

  return (
    <div className="select" ref={root}>
      {name && <input type="hidden" name={name} value={value ?? ""} />}
      <button
        type="button"
        className={`select-trigger${selected ? "" : " placeholder"}`}
        onClick={() => { if (disabled) return; if (open) close(); else openMenu(); }}
        onKeyDown={onTriggerKey}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
      >
        <span className="select-value">{selected?.label ?? placeholder}</span>
        <ChevronDown className="select-caret" />
      </button>

      {open && (
        <div className="select-menu" role="listbox" ref={menu} aria-label={ariaLabel}>
          {options.length === 0 && <div className="select-option" aria-disabled="true">No options</div>}
          {options.map((option, i) => (
            <button
              type="button"
              key={option.value}
              className={`select-option${i === active ? " active" : ""}`}
              role="option"
              aria-selected={option.value === value}
              onMouseEnter={() => setActive(i)}
              onClick={() => pick(option)}
            >
              <Check className="select-check" />
              <span className="select-label">{option.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
