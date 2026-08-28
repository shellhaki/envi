import { upstream } from "@/lib/server-api";
export async function POST(request: Request) {
  const response = await upstream("/auth/request-otp", { method: "POST", body: await request.text() });
  return new Response(await response.text(), { status: response.status, headers: { "Content-Type": "application/json" } });
}
