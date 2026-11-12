import type { Config } from "./config";
import type { Cipher } from "./crypto";
import type { Db } from "./db";
import type { Mailer } from "./mail";
import type { Redis } from "./redis";
import type { Bachs } from "./services/bachs";
import type { Users } from "./services/instance";

// Deps is everything a request needs. Each entry file (server.ts, worker.ts) builds it.
export type Deps = {
  config: Config;
  db: Db;
  redis: Redis;
  cipher: Cipher;
  mailer: Mailer;
  bachs: Bachs | null; // null when billing is off
  users: Users | null; // the users file of a self-hosted instance, or null for email sign-in
  // background keeps work running after the response is sent (waitUntil on Workers).
  background(work: Promise<unknown>): void;
  now(): Date;
};

export type AppEnv = {
  Bindings: { deps: Deps };
  Variables: {
    userId: string;
    userPermission: string;
    serviceId: string;
    serviceEnv: string;
    servicePermission: string;
    operatorRole: string;
    operatorEmail: string;
  };
};
