import {
  EnviAuthError,
  EnviError,
  EnviMissingKeyError,
  EnviNotFoundError,
  EnviUnreachableError,
} from "./errors.js";

/**
 * How to reach Envi, and as whom.
 *
 * Give either a service token or an API key, not both:
 *
 *   token   A service token. It already belongs to one project, so there is
 *           nothing else to configure, and a leaked one reaches that project
 *           only. Use this in production.
 *
 *   apiKey  Your personal key, which can reach every project you can, so it
 *           also needs `project`. Handy while developing.
 */
export type EnviOptions = {
  token?: string;
  apiKey?: string;
  project?: string;
  /** Defaults to the hosted API; point this at your own instance if you self-host. */
  baseUrl?: string;
  /** How long to wait for a response, in milliseconds. Default 10000. */
  timeoutMs?: number;
  /** Supply your own fetch, mainly for tests. */
  fetch?: typeof globalThis.fetch;
};

const DEFAULT_BASE_URL = "https://api.envisecrets.com";
const DEFAULT_TIMEOUT_MS = 10_000;

/**
 * A key-value store for one Envi project.
 *
 *   const envi = new Envi({ token: process.env.ENVI_TOKEN! });
 *   await envi.set("THEME", "dark");
 *   await envi.get("THEME");        // "dark"
 *
 * This is not your secrets. Secrets belong to an environment, keep a version
 * history, and are pulled into a process by `envi run` or `envi pull`. These are
 * plain project-wide values an application reads and writes while it runs.
 *
 * Values are loaded once and held in memory, so repeated reads cost nothing.
 * Writes go to the server and update what's held. Call `refresh()` to pick up
 * changes made elsewhere.
 *
 * Server-side only. Constructing this where a browser can reach it would put
 * your credential, and everything it can read, into the bundle you ship to
 * visitors, so it refuses to run there.
 */
export class Envi {
  readonly #options: EnviOptions;
  readonly #baseUrl: string;
  readonly #fetch: typeof globalThis.fetch;
  readonly #timeoutMs: number;

  #values: Map<string, string> | null = null;
  #project = "";
  #loading: Promise<void> | null = null;
  #session: { access: string; refresh: string } | null = null;
  #refreshing: Promise<boolean> | null = null;

  constructor(options: EnviOptions) {
    // `window` is not declared in a Node type environment, so ask globalThis
    // for it by name rather than as a property.
    if (typeof (globalThis as Record<string, unknown>)["window"] !== "undefined") {
      throw new EnviError_BrowserUse();
    }
    if (!options.token && !options.apiKey) {
      throw new EnviAuthError("Pass either a service token (token) or a personal key (apiKey).");
    }
    if (options.token && options.apiKey) {
      throw new EnviAuthError("Pass a service token or a personal key, not both.");
    }
    if (options.apiKey && !options.project) {
      throw new EnviAuthError(
        "A personal key can reach several projects, so `project` is required. A service token needs no project.",
      );
    }

    this.#options = options;
    this.#baseUrl = (options.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, "");
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    const fetchImpl = options.fetch ?? globalThis.fetch;
    if (typeof fetchImpl !== "function") {
      throw new EnviError_NoFetch();
    }
    this.#fetch = fetchImpl;
  }

  /** A value, or undefined if it isn't set. Loads on first use. */
  async get(key: string): Promise<string | undefined> {
    await this.ready();
    return this.#values!.get(key);
  }

  /** The same, but throws naming the key. For checks at start-up. */
  async require(key: string): Promise<string> {
    const value = await this.get(key);
    if (value === undefined) {
      throw new EnviMissingKeyError(`${key} is not set in ${this.#project}.`);
    }
    return value;
  }

  /** Writes a value, replacing anything already under that key. */
  async set(key: string, value: string): Promise<void> {
    await this.ready();
    await this.request("PUT", "/kv", { project: this.#options.project, key, value });
    this.#values!.set(key, value);
  }

  /** Removes a key. Deleting one that isn't there is not an error. */
  async delete(key: string): Promise<void> {
    await this.ready();
    await this.request("DELETE", "/kv", { project: this.#options.project, key });
    this.#values!.delete(key);
  }

  /** Every value, as a plain object that can be modified freely. */
  async all(): Promise<Record<string, string>> {
    await this.ready();
    return Object.fromEntries(this.#values!);
  }

  /** The keys currently set, sorted. */
  async keys(): Promise<string[]> {
    await this.ready();
    return [...this.#values!.keys()].sort();
  }

  /** Loads the values. Safe to call repeatedly; only the first one fetches. */
  async ready(): Promise<void> {
    if (this.#values) return;
    this.#loading ??= this.#load().finally(() => {
      this.#loading = null;
    });
    return this.#loading;
  }

  /** Fetches again, replacing what's held. */
  async refresh(): Promise<void> {
    await this.#load();
  }

  /** Which project these values belong to. Available after the first load. */
  get project(): string {
    return this.#project;
  }

  async #load(): Promise<void> {
    const query = this.#options.project
      ? `?project=${encodeURIComponent(this.#options.project)}`
      : "";
    const body = (await this.request("GET", `/kv${query}`)) as {
      project: string;
      values: Record<string, string>;
    };
    this.#project = body.project ?? this.#options.project ?? "";
    this.#values = new Map(Object.entries(body.values ?? {}));
  }

  /** One request, with a session renewal and a single retry on a 401. */
  private async request(method: string, path: string, payload?: unknown): Promise<unknown> {
    let response = await this.#send(method, path, await this.#authorization(), payload);
    // A session from an API key lasts fifteen minutes; a 401 usually just means
    // this one aged out while the process was idle.
    if (response.status === 401 && this.#options.apiKey && (await this.#renewSession())) {
      response = await this.#send(method, path, await this.#authorization(), payload);
    }

    if (response.status === 401 || response.status === 403) {
      throw new EnviAuthError(await this.#message(response, "Envi rejected the credential."));
    }
    if (response.status === 404) {
      throw new EnviNotFoundError(await this.#message(response, "No such project."));
    }
    if (!response.ok) {
      throw new EnviUnreachableError(await this.#message(response, `Envi answered ${response.status}.`));
    }
    if (response.status === 204) return null;
    return response.json().catch(() => null);
  }

  async #authorization(): Promise<string> {
    if (this.#options.token) return this.#options.token;
    if (!this.#session) await this.#renewSession();
    return this.#session?.access ?? "";
  }

  /**
   * Exchanges the API key for a session, or renews an existing one.
   *
   * Single-flight on purpose: refresh tokens may be spent once, so two requests
   * renewing at the same moment would race, and the loser would be told its
   * perfectly good session had expired.
   */
  async #renewSession(): Promise<boolean> {
    this.#refreshing ??= this.#doRenewSession().finally(() => {
      this.#refreshing = null;
    });
    return this.#refreshing;
  }

  async #doRenewSession(): Promise<boolean> {
    const [path, payload] = this.#session
      ? (["/auth/refresh", { refresh_token: this.#session.refresh }] as const)
      : (["/auth/api-key", { key: this.#options.apiKey }] as const);

    const response = await this.#send("POST", path, undefined, payload);
    if (!response.ok) {
      // A failed renewal of an old session is worth one attempt from scratch:
      // the session may simply have outlived its refresh window.
      if (this.#session) {
        this.#session = null;
        return this.#doRenewSession();
      }
      throw new EnviAuthError(
        await this.#message(response, "Envi rejected the API key. It may be wrong, expired, or revoked."),
      );
    }
    const body = (await response.json()) as { access_token?: string; refresh_token?: string };
    if (!body.access_token || !body.refresh_token) {
      throw new EnviAuthError("Envi did not return a session.");
    }
    this.#session = { access: body.access_token, refresh: body.refresh_token };
    return true;
  }

  async #send(method: string, path: string, bearer?: string, body?: unknown): Promise<Response> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);
    try {
      return await this.#fetch(this.#baseUrl + path, {
        method,
        headers: {
          ...(bearer ? { Authorization: `Bearer ${bearer}` } : {}),
          ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (cause) {
      const timedOut = controller.signal.aborted;
      throw new EnviUnreachableError(
        timedOut
          ? `Envi did not answer within ${this.#timeoutMs}ms (${this.#baseUrl}).`
          : `Could not reach Envi at ${this.#baseUrl}.`,
        cause,
      );
    } finally {
      clearTimeout(timer);
    }
  }

  /** Prefers the server's own explanation, falling back to a generic one. */
  async #message(response: Response, fallback: string): Promise<string> {
    const body = (await response.json().catch(() => null)) as { error?: string } | null;
    return body?.error ? body.error : fallback;
  }
}

// Declared after the class so the constructor can throw them by name without
// cluttering the public error list: these are mistakes in how the SDK is used,
// not conditions an application handles.
class EnviError_BrowserUse extends EnviError {
  constructor() {
    super(
      "The Envi SDK is server-side only. Importing it into browser code would ship your credential and everything it can read to visitors. Read values on the server and pass down only what the page needs.",
    );
  }
}
class EnviError_NoFetch extends EnviUnreachableError {
  constructor() {
    super("No fetch available. Use Node 18 or newer, or pass your own via the `fetch` option.");
  }
}
