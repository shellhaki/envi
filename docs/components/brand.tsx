import Image from "next/image";
import { SITE_URL } from "@/lib/docs-nav";

/** On a docs subdomain the wordmark points back at the main site. */
export default function Brand() {
  return (
    <a className="brand" href={SITE_URL} aria-label="Envi home">
      <Image className="brand-logo brand-logo-light" src="/envi-logo-with-text-light-mode.png" alt="" width={150} height={50} priority />
      <Image className="brand-logo brand-logo-dark" src="/envi-logo-with-text-dark-mode.png" alt="" width={150} height={50} priority />
    </a>
  );
}
