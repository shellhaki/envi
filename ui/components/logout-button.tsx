"use client";
import { LogOut } from "lucide-react";
import { useRouter } from "next/navigation";
export default function LogoutButton() { const router = useRouter(); return <button onClick={async () => { await fetch("/api/auth/logout", { method: "POST" }); router.push("/"); router.refresh(); }}><LogOut />Sign out</button>; }
