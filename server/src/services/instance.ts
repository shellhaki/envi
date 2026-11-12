// The users file of a self-hosted instance: who may sign in, their role, and a hash of their password.
// A password written in plain text (to reset it) is hashed and the file rewritten the next time it is read.

import { randomBelow } from "../crypto";
import { Invalid } from "../errors";

export type Role = "admin" | "contributor";
export type FileUser = { email: string; role: Role; hash: string };

export interface Users {
  list(): Promise<FileUser[]>;
  find(email: string): Promise<FileUser | undefined>;
  verify(email: string, password: string): Promise<FileUser | undefined>;
  // add and resetPassword return the new password; it is shown once and never stored.
  add(email: string): Promise<string>;
  resetPassword(email: string): Promise<string>;
  setPassword(email: string, password: string): Promise<void>;
  remove(email: string): Promise<void>;
}

const HEADER = `# The people who can sign in to this Envi instance.
#
# Forgot a password? Replace that person's hash line with
#   password = "a new password"
# then sign in with it. Envi hashes it and rewrites the line.
`;

export const MIN_PASSWORD_LENGTH = 10;

type Entry = { email?: unknown; role?: unknown; hash?: unknown; password?: unknown };

// usersFile keeps the users in a TOML file at path. Bun only: the hosted Worker signs in by email instead.
export function usersFile(path: string): Users {
  let writing = Promise.resolve();

  async function load(): Promise<FileUser[]> {
    const file = Bun.file(path);
    if (!(await file.exists())) throw new Error(`the users file ${path} does not exist`);
    const parsed = Bun.TOML.parse(await file.text()) as { user?: Entry[] };
    const users: FileUser[] = [];
    let changed = false;
    for (const entry of parsed.user ?? []) {
      const email = normalise(String(entry.email ?? ""));
      if (!email) continue;
      const role: Role = entry.role === "admin" ? "admin" : "contributor";
      let hash = typeof entry.hash === "string" ? entry.hash : "";
      if (typeof entry.password === "string" && entry.password) {
        hash = await Bun.password.hash(entry.password, "bcrypt");
        changed = true;
      }
      users.push({ email, role, hash });
    }
    if (changed) await save(users);
    return users;
  }

  async function save(users: FileUser[]) {
    const body = users.map((u) => `[[user]]\nemail = ${JSON.stringify(u.email)}\nrole = ${JSON.stringify(u.role)}\nhash = ${JSON.stringify(u.hash)}\n`);
    const text = `${HEADER}\n${body.join("\n")}`;
    writing = writing.then(() => Bun.write(path, text).then(() => undefined));
    await writing;
  }

  async function update(email: string, change: (users: FileUser[], index: number) => void) {
    const users = await load();
    const index = users.findIndex((u) => u.email === normalise(email));
    change(users, index);
    await save(users);
  }

  return {
    list: load,
    async find(email) {
      return (await load()).find((u) => u.email === normalise(email));
    },
    async verify(email, password) {
      const user = (await load()).find((u) => u.email === normalise(email));
      if (!user?.hash || !password) return undefined;
      return (await Bun.password.verify(password, user.hash).catch(() => false)) ? user : undefined;
    },
    async add(email) {
      const clean = normalise(email);
      if (!/^[^\s@]+@[^\s@]+$/.test(clean)) throw new Invalid("a valid email address is required");
      const password = generatePassword();
      const hash = await Bun.password.hash(password, "bcrypt");
      await update(clean, (users, index) => {
        if (index !== -1) throw new Invalid(`${clean} can already sign in`);
        users.push({ email: clean, role: "contributor", hash });
      });
      return password;
    },
    async resetPassword(email) {
      const password = generatePassword();
      await this.setPassword(email, password);
      return password;
    },
    async setPassword(email, password) {
      if (password.length < MIN_PASSWORD_LENGTH) throw new Invalid(`a password needs at least ${MIN_PASSWORD_LENGTH} characters`);
      const hash = await Bun.password.hash(password, "bcrypt");
      await update(email, (users, index) => {
        if (index === -1) throw new Invalid(`${normalise(email)} is not a user of this instance`);
        users[index].hash = hash;
      });
    },
    async remove(email) {
      await update(email, (users, index) => {
        if (index === -1) throw new Invalid(`${normalise(email)} is not a user of this instance`);
        if (users[index].role === "admin") throw new Invalid("the admin can't be removed");
        users.splice(index, 1);
      });
    },
  };
}

// generatePassword is 16 letters and digits in four groups, easy to read out: "k7qm-2xdp-9fwa-h3nr".
export function generatePassword(): string {
  const alphabet = "abcdefghjkmnpqrstuvwxyz23456789";
  let out = "";
  for (let i = 0; i < 16; i++) {
    if (i > 0 && i % 4 === 0) out += "-";
    out += alphabet[randomBelow(alphabet.length)];
  }
  return out;
}

function normalise(email: string) {
  return email.trim().toLowerCase();
}
