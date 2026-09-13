import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emits .next/standalone with a self-contained server.js, so the Docker
  // runtime image doesn't need node_modules. Ignored by Vercel.
  output: "standalone",
};

export default nextConfig;
