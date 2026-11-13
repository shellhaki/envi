# The API server on Bun, for a self-hosted instance. The hosted API runs on Cloudflare Workers instead.
FROM oven/bun:1-alpine
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production
COPY tsconfig.json ./
COPY src ./src
ENV NODE_ENV=production
EXPOSE 8080
CMD ["bun", "src/server.ts"]
