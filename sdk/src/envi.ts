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
 *   token   A service token, which is already tied to one environment. Nothing
 *           else to configure, and a leaked one exposes that environment only.
 *           This is the one to use in production.
 *
 *   apiKey  Your personal key, which can reach everything you can, so it also
 *           needs `project` and (unless the project has exactly one)
 *           `environment`. Handy while developing.
 */
export type EnviOptions = {
  token?: string;
  apiKey?: string;
  project?: string;
  environment?: string;
  /** Defaults to the hosted API; point this at your own instance if you self-host. */
  baseUrl?: string;
  /** How long to wait for a response, in milliseconds. Default 10000. */
  timeoutMs?: number;
  /** Supply your own fetch, mainly for tests. */
  fetch?: typeof globalThis.fetch;
};

type Snapshot = {
  project: string;
  environment: string;
  values: Record<string, string>;
  revision: number;
};

const DEFAULT_BASE_URL = "https://api.envisecrets.com";
const DEFAULT_TIMEOUT_MS = 10_000;

/**
 * Reads the secrets of one Envi environment.
 *
 *   const envi = new Envi({ token: process.env.ENVI_TOKEN! });
 *   await envi.ready();
 *   envi.get("DATABASE_URL");
 *
 * Values are fetched once and held in memory, so `get` never waits on the
 * network and there are no timers running in the background. Call `refresh()`
 * when you want to pick up changes.
 *
 * Server-side only. Constructing this in code that reaches a browser would put
 * your credential, and every secret it can read, into the bundle you ship to
 * visitors, so it refuses to run there.
 */
export class Envi {
  readonly #options: EnviOptions;
  readonly #baseUrl: string;
  readonly #fetch: typeof globalThis.fetch;
  readonly #timeoutMs: number;

  #snapshot: Snapshot | null = null;
  /** The in-flight first load, so concurrent ready() calls share one request. */
  #loading: Promise<void> | null = null;
  /** A session obtained by exchanging an API key, and the refresh in flight. */
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

  /** Loads the values. Safe to call repeatedly; only the first one fetches. */
  async ready(): Promise<void> {
    if (this.#snapshot) return;
    this.#loading ??= this.#load().finally(() => {
      this.#loading = null;
    });
    return this.#loading;
  }

  /** Fetches again, replacing what's held. */
  async refresh(): Promise<void> {
    await this.#load();
  }

  /** A value, or undefined. Reads memory; call ready() first. */
  get(key: string): string | undefined {
    return this.#loaded().values[key];
  }

  /** A value, or an error naming the key. For checks at start-up. */
  require(key: string): string {
    const value = this.get(key);
    if (value === undefined) {
      throw new EnviMissingKeyError(
        `${key} is not set in ${this.#loaded().project}/${this.#loaded().environment}.`,
      );
    }
    return value;
  }

  /** Every value, as a copy that cannot be modified. */
  all(): Readonly<Record<string, string>> {
    return Object.freeze({ ...this.#loaded().values });
  }

  /** Which project and environment these values came from, and their revision. */
  get source(): { project: string; environment: string; revision: number } {
    const { project, environment, revision } = this.#loaded();
    return { project, environment, revision };
  }

  #loaded(): Snapshot {
    if (!this.#snapshot) {
      throw new EnviError_NotReady();
    }
    return this.#snapshot;
  }

  async #load(): Promise<void> {
    const query = new URLSearchParams();
    if (this.#options.project) query.set("project", this.#options.project);
    if (this.#options.environment) query.set("environment", this.#options.environment);
    const search = query.toString();
    const path = `/values${search ? `?${search}` : ""}`;

    let response = await this.#send(path, await this.#authorization());
    // A session from an API key lasts fifteen minutes; a 401 usually just means
    // this one aged out while the process was idle.
    if (response.status === 401 && this.#options.apiKey && (await this.#renewSession())) {
      response = await this.#send(path, await this.#authorization());
    }

    if (response.status === 401 || response.status === 403) {
      throw new EnviAuthError(await this.#message(response, "Envi rejected the credential."));
    }
    if (response.status === 404) {
      throw new EnviNotFoundError(await this.#message(response, "No such project or environment."));
    }
    if (!response.ok) {
      throw new EnviUnreachableError(
        await this.#message(response, `Envi answered ${response.status}.`),
      );
    }

    const body = (await response.json()) as Snapshot;
    this.#snapshot = {
      project: body.project,
      environment: body.environment,
      values: body.values ?? {},
      revision: body.revision ?? 0,
    };
  }

  /** The Authorization header value, obtaining a session first if needed. */
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

    const response = await this.#send(path, undefined, payload);
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

  async #send(path: string, bearer?: string, body?: unknown): Promise<Response> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);
    try {
      return await this.#fetch(this.#baseUrl + path, {
        method: body === undefined ? "GET" : "POST",
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
      "The Envi SDK is server-side only. Importing it into browser code would ship your credential and every secret it can read to visitors. Read secrets on the server and pass down only what the page needs.",
    );
  }
}
class EnviError_NoFetch extends EnviUnreachableError {
  constructor() {
    super("No fetch available. Use Node 18 or newer, or pass your own via the `fetch` option.");
  }
}
class EnviError_NotReady extends EnviError {
  constructor() {
    super("Call `await envi.ready()` before reading values.");
  }
}
