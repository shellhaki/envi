"use client";
import { LogOut } from "lucide-react";
import { useRouter } from "next/navigation";
export default function LogoutButton() { const router = useRouter(); return <button title="Sign out" onClick={async () => { await fetch("/api/auth/logout", { method: "POST" }); router.push("/"); router.refresh(); }}><LogOut /><span className="logout-label">Sign out</span></button>; }
