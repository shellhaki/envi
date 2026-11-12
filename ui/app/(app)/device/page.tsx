import Brand from "@/components/brand";
import ThemeToggle from "@/components/theme-toggle";
import { account } from "@/lib/server-api";
import { redirect } from "next/navigation";
import DeviceForm from "./device-form";

export default async function DevicePage({ searchParams }: { searchParams: Promise<{ code?: string | string[] }> }) {
  const raw = (await searchParams).code;
  const code = (Array.isArray(raw) ? raw[0] : raw) ?? "";
  const user = await account();
  if (!user) {
    // Bounce through login, then return here with the code still in the URL.
    const next = code ? `/device?code=${encodeURIComponent(code)}` : "/device";
    redirect(`/auth?next=${encodeURIComponent(next)}`);
  }
  return <main className="auth-page"><header><Brand /><ThemeToggle /></header><section className="auth-box"><span className="kicker">Device authorization</span><h1>Connect your terminal</h1><p>Signed in as {user.Email}. Enter the code shown in your terminal to authorize the Envi CLI on this device.</p><DeviceForm initialCode={code} /><small>Only approve a code you started yourself from the Envi CLI.</small></section></main>;
}
