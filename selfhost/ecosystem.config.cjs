// pm2 processes for a self-hosted Envi with ENVI_RUNTIME=pm2. Postgres and Redis run in Docker (compose.yml).
// Settings come from the .env file next to this one.
const fs = require("node:fs");
const path = require("node:path");

const root = __dirname;
const env = Object.fromEntries(
  fs.readFileSync(path.join(root, ".env"), "utf8").split("\n")
    .filter((line) => /^[A-Z_]+=/.test(line))
    .map((line) => [line.slice(0, line.indexOf("=")), line.slice(line.indexOf("=") + 1)]),
);
const host = env.PUBLISH_HOST || "127.0.0.1";
const apiPort = env.API_PORT || "8080";

module.exports = {
  apps: [
    {
      name: "envi-api",
      cwd: path.join(root, "src/server"),
      // bun itself is the program: pm2's Bun wrapper loads scripts with require(), which can't load top-level await.
      script: env.BUN_PATH || "bun",
      args: "src/server.ts",
      interpreter: "none",
      env: {
        ...env,
        NODE_ENV: "production",
        DATABASE_URL: `postgres://envi:${env.POSTGRES_PASSWORD}@127.0.0.1:${env.POSTGRES_PORT || 55432}/envi`,
        REDIS_URL: `redis://127.0.0.1:${env.REDIS_PORT || 56379}`,
        ENVI_USERS_FILE: path.join(root, "data/users.toml"),
        ENVI_API_PORT: apiPort,
        ENVI_API_HOST: host,
      },
    },
    {
      name: "envi-web",
      cwd: path.join(root, "src/web/.next/standalone"),
      script: "server.js",
      env: {
        NODE_ENV: "production",
        ENVI_API_URL: `http://127.0.0.1:${apiPort}`,
        ENVI_SESSION_SECRET: env.ENVI_SESSION_SECRET,
        ENVI_PLAIN_HTTP: env.ENVI_PLAIN_HTTP || "0",
        PORT: env.WEB_PORT || "3000",
        HOSTNAME: host,
      },
    },
  ],
};
