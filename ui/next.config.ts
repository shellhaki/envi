import type { NextConfig } from "next";

// Standalone output emits .next/standalone with a self-contained server.js, so
// the Docker runtime image doesn't need node_modules.
//
// It is deliberately NOT set on Vercel. Standalone changes where Next's file
// trace output lands, and Vercel's own onBuildComplete step then fails with
// ENOENT on .next/next-server.js.nft.json. VERCEL is set in their build env,
// so the platform that needs it gets it and the one it breaks does not.
const nextConfig: NextConfig = process.env.VERCEL
  ? {}
  : { output: "standalone" };

export default nextConfig;
