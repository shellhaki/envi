import createMDX from "@next/mdx";
import type { NextConfig } from "next";

// Standalone output emits .next/standalone with a self-contained server.js: the Docker image runs it,
// and OpenNext builds the Cloudflare Worker from it. Images are served as they are, since the Worker has no optimizer.
const nextConfig: NextConfig = {
  // The docs pages (app/docs) are .mdx.
  pageExtensions: ["ts", "tsx", "mdx"],
  output: "standalone",
  images: { unoptimized: true },
};

export default createMDX({})(nextConfig);
