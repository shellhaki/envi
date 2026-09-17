import createMDX from "@next/mdx";
import type { NextConfig } from "next";

// Standalone output is what lets the Docker runtime image drop node_modules.
// It is deliberately NOT set on Vercel: it moves Next's file trace output and
// Vercel's own build step then fails with ENOENT on
// .next/next-server.js.nft.json. VERCEL is set in their build environment.
const nextConfig: NextConfig = {
  pageExtensions: ["ts", "tsx", "mdx"],
  ...(process.env.VERCEL ? {} : { output: "standalone" as const }),
};

const withMDX = createMDX({});

export default withMDX(nextConfig);
