import { redirect } from "next/navigation";
import { instance } from "@/lib/server-api";

// A self-hosted instance has no billing: everything on it is free.
export default async function BillingLayout({ children }: { children: React.ReactNode }) {
  if (!(await instance()).billing) redirect("/dashboard");
  return children;
}
