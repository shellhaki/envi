"use client";
import { Children, isValidElement, useState, type ReactElement } from "react";
import Link from "next/link";
import { AlertTriangle, Check, ChevronRight, Info, Lightbulb } from "lucide-react";

/* ---------------------------------------------------------------- cards -- */

export function CardGroup({ cols = 2, children }: { cols?: number; children: React.ReactNode }) {
  return <div className="doc-cards" style={{ "--doc-card-cols": cols } as React.CSSProperties}>{children}</div>;
}

export function Card({ title, href, icon, children }: { title: string; href?: string; icon?: React.ReactNode; children?: React.ReactNode }) {
  const body = (
    <>
      {icon && <span className="doc-card-icon">{icon}</span>}
      <strong>{title}</strong>
      {children && <span className="doc-card-body">{children}</span>}
    </>
  );
  return href ? <Link className="doc-card" href={href}>{body}</Link> : <div className="doc-card">{body}</div>;
}

/* ------------------------------------------------------------- callouts -- */

function Callout({ kind, icon, children }: { kind: string; icon: React.ReactNode; children: React.ReactNode }) {
  return <div className={`doc-callout ${kind}`}><span className="doc-callout-icon">{icon}</span><div>{children}</div></div>;
}
export const Note = ({ children }: { children: React.ReactNode }) => <Callout kind="note" icon={<Info />}>{children}</Callout>;
export const Warning = ({ children }: { children: React.ReactNode }) => <Callout kind="warning" icon={<AlertTriangle />}>{children}</Callout>;
export const Tip = ({ children }: { children: React.ReactNode }) => <Callout kind="tip" icon={<Lightbulb />}>{children}</Callout>;

/* ---------------------------------------------------------------- steps -- */

export function Steps({ children }: { children: React.ReactNode }) {
  return <ol className="doc-steps">{children}</ol>;
}
export function Step({ title, children }: { title: string; children: React.ReactNode }) {
  return <li className="doc-step"><strong className="doc-step-title">{title}</strong><div className="doc-step-body">{children}</div></li>;
}

/* ----------------------------------------------------------------- tabs -- */

export function Tabs({ children }: { children: React.ReactNode }) {
  // MDX puts whitespace text nodes between elements. Reading `.props` off a
  // raw children array picks those up as blank tabs and renders differently
  // on server vs client, which is a hydration mismatch — filter to elements.
  const items = Children.toArray(children).filter(isValidElement) as ReactElement<{ title?: string }>[];
  const labels = items.map((c) => c.props.title ?? "");
  const [active, setActive] = useState(0);
  return (
    <div className="doc-tabs">
      <div className="doc-tab-list" role="tablist">
        {labels.map((label, i) => (
          <button key={label + i} role="tab" aria-selected={i === active} className={i === active ? "active" : undefined} onClick={() => setActive(i)}>
            {label}
          </button>
        ))}
      </div>
      <div className="doc-tab-panel">{items[active]}</div>
    </div>
  );
}
export function Tab({ children }: { title: string; children: React.ReactNode }) {
  return <>{children}</>;
}

/* ----------------------------------------------------------- accordions -- */

export function AccordionGroup({ children }: { children: React.ReactNode }) {
  return <div className="doc-accordions">{children}</div>;
}
export function Accordion({ title, icon, children }: { title: string; icon?: React.ReactNode; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className={`doc-accordion${open ? " open" : ""}`}>
      <button onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <ChevronRight className="doc-accordion-caret" />
        {icon && <span className="doc-accordion-icon">{icon}</span>}
        <span>{title}</span>
      </button>
      {open && <div className="doc-accordion-body">{children}</div>}
    </div>
  );
}

/* ------------------------------------------------------------- params ---- */

export function ParamField({ query, type, required, default: def, children }: { query: string; type?: string; required?: boolean; default?: string; children?: React.ReactNode }) {
  return (
    <div className="doc-param">
      <div className="doc-param-head">
        <code>{query}</code>
        {type && <span className="doc-param-type">{type}</span>}
        {def && <span className="doc-param-default">default: {def}</span>}
        {required && <span className="doc-param-required">required</span>}
      </div>
      {children && <div className="doc-param-body">{children}</div>}
    </div>
  );
}

/* ------------------------------------------------------------- diagram --- */

/** Replaces the Mermaid fences from the Mintlify source. Hand-built so it
 *  matches the site's type and colour instead of a generic default render. */
export function Diagram({ nodes, caption }: { nodes: { from: string; label?: string; to: string }[]; caption?: string }) {
  return (
    <div className="doc-diagram">
      {nodes.map((n, i) => (
        <div className="doc-diagram-row" key={i}>
          <span className="doc-diagram-node">{n.from}</span>
          <span className="doc-diagram-arrow">{n.label && <em>{n.label}</em>}<ChevronRight /></span>
          <span className="doc-diagram-node">{n.to}</span>
        </div>
      ))}
      {caption && <p className="doc-diagram-caption">{caption}</p>}
    </div>
  );
}

export const DocCheck = () => <Check className="doc-inline-check" />;
