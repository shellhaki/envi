# The documentation site (Next.js + MDX).
#
# Coolify settings:
#   Build Pack        Dockerfile
#   Dockerfile        Dockerfile
#   Base Directory    /docs
#   Ports Exposes     3000
#
# The build context is this directory, so paths below are relative to /docs.

FROM oven/bun:1 AS deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

FROM oven/bun:1 AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
# `bunx --bun` because the image has no node for a package script to shell out to.
RUN bunx --bun next build

# server.js from `output: "standalone"` is a plain Node program, so the runtime
# image needs neither bun nor node_modules.
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
RUN addgroup -g 1001 -S nodejs && adduser -S nextjs -u 1001
COPY --from=build --chown=nextjs:nodejs /app/.next/standalone ./
# standalone deliberately omits these two; without them every asset 404s.
COPY --from=build --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=build --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
