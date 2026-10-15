import { redirect } from "next/navigation";
import { account } from "@/lib/server-api";

/*
  The operator screen deliberately does not use the dashboard layout.

  That layout mounts Workspace, which loads projects, secrets and activity for
  the signed-in user's own organization — none of which belongs on a screen
  about the whole instance, and all of which would fire on every visit.

  This only checks that somebody is signed in. Whether they are an operator is
  the API's decision, and the page 404s on anything but a clean answer.
*/
export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  if (!(await account())) redirect("/auth");
  return <div className="admin-shell">{children}</div>;
}
