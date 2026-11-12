import { redirect } from "next/navigation";
import { account } from "@/lib/server-api";

// Only a self-hosted instance's admin manages its users. The API refuses everyone else too.
export default async function UsersLayout({ children }: { children: React.ReactNode }) {
  if ((await account())?.Role !== "admin") redirect("/dashboard");
  return children;
}
