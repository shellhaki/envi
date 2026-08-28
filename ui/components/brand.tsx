import Image from "next/image";
import Link from "next/link";

export default function Brand() {
  return <Link className="brand" href="/" aria-label="Envi home">
    <Image className="brand-logo brand-logo-light" src="/envi-logo-with-text-light-mode.png" alt="" width={150} height={50} priority />
    <Image className="brand-logo brand-logo-dark" src="/envi-logo-with-text-dark-mode.png" alt="" width={150} height={50} priority />
  </Link>;
}
