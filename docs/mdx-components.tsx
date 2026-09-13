import type { MDXComponents } from "mdx/types";
import Link from "next/link";
import {
  Accordion, AccordionGroup, Card, CardGroup, Diagram, Note,
  ParamField, Step, Steps, Tab, Tabs, Tip, Warning,
} from "@/components/docs-ui";

export function useMDXComponents(components: MDXComponents): MDXComponents {
  return {
    // Internal links go through next/link so docs navigation stays client-side.
    a: ({ href, children, ...rest }) => {
      const url = String(href ?? "");
      return url.startsWith("/")
        ? <Link href={url} {...rest}>{children}</Link>
        : <a href={url} target="_blank" rel="noreferrer" {...rest}>{children}</a>;
    },
    // Components usable in .mdx without an import.
    Accordion, AccordionGroup, Card, CardGroup, Diagram, Note,
    ParamField, Step, Steps, Tab, Tabs, Tip, Warning,
    ...components,
  };
}
