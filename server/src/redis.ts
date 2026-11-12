import { Redis as Upstash } from "@upstash/redis";

// Redis is the small key-value API Envi uses: login codes, counters, and later caching.
export interface Redis {
  get(key: string): Promise<string | null>;
  set(key: string, value: string, ttlSeconds: number): Promise<void>;
  del(key: string): Promise<void>;
  // incr adds one to a counter, starting its expiry on the first increment.
  incr(key: string, ttlSeconds: number): Promise<number>;
}

// Upstash over HTTPS, which works on Cloudflare Workers and everywhere else.
export function upstashRedis(url: string, token: string): Redis {
  const client = new Upstash({ url, token, automaticDeserialization: false });
  return {
    async get(key) {
      return (await client.get<string>(key)) ?? null;
    },
    async set(key, value, ttlSeconds) {
      await client.set(key, value, { ex: ttlSeconds });
    },
    async del(key) {
      await client.del(key);
    },
    async incr(key, ttlSeconds) {
      const n = await client.incr(key);
      if (n === 1) await client.expire(key, ttlSeconds);
      return n;
    },
  };
}

// A plain Redis server through Bun's built-in client, for self-hosting.
export function bunRedis(url: string): Redis {
  const client = new Bun.RedisClient(url);
  return {
    get: (key) => client.get(key),
    async set(key, value, ttlSeconds) {
      await client.set(key, value, "EX", ttlSeconds);
    },
    async del(key) {
      await client.del(key);
    },
    async incr(key, ttlSeconds) {
      const n = await client.incr(key);
      if (n === 1) await client.expire(key, ttlSeconds);
      return n;
    },
  };
}

// In memory, for tests.
export function memoryRedis(): Redis {
  const values = new Map<string, { value: string; expires: number }>();
  const live = (key: string) => {
    const entry = values.get(key);
    if (entry && entry.expires > Date.now()) return entry;
    values.delete(key);
    return undefined;
  };
  return {
    async get(key) {
      return live(key)?.value ?? null;
    },
    async set(key, value, ttlSeconds) {
      values.set(key, { value, expires: Date.now() + ttlSeconds * 1000 });
    },
    async del(key) {
      values.delete(key);
    },
    async incr(key, ttlSeconds) {
      const entry = live(key);
      const n = (entry ? Number(entry.value) : 0) + 1;
      values.set(key, { value: String(n), expires: entry?.expires ?? Date.now() + ttlSeconds * 1000 });
      return n;
    },
  };
}
