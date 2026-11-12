import Link from "next/link";
import { cookies } from "next/headers";
import { Gauge } from "lucide-react";
import { upstream } from "@/lib/server-api";
import { readSession, sessionCookie } from "@/lib/web-session";

/*
  The link to the operator screen, shown only to operators.

  Asked on the server, so a non-operator's HTML never contains the link at all.
  Deciding this in the browser would put the answer in a response everybody
  receives and leave the markup as the only thing hiding it.

  This is a convenience, not a control: /admin and /admin/stats both refuse on
  their own. Someone who guesses the URL still gets a 404.
*/
export default async function OperatorLink() {
  const session = readSession((await cookies()).get(sessionCookie)?.value);
  if (!session) return null;

  // A failure here means "no link", never a broken sidebar: the operator
  // screen is not worth taking the dashboard down for.
  const response = await upstream("/admin/whoami", {
    headers: { Authorization: `Bearer ${session.access}` },
  }).catch(() => null);
  if (!response?.ok) return null;

  return (
    <>
      <span className="sidebar-label">Operator</span>
      <Link href="/admin" title="Instance">
        <Gauge />
        <span className="nav-label">Instance</span>
      </Link>
    </>
  );
}
