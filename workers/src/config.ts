import { ApiError } from "./errors";
import type { Env } from "./types";

const isLoopback = (url: string) =>
  url.includes("localhost") || url.includes("127.0.0.1") || url.includes("[::1]");

/** Unset, or a development label, means local. Anything else serves real users. */
const isDeployed = (environment: string) =>
  !["", "development", "dev", "local", "test"].includes(environment.trim().toLowerCase());

/**
 * Invitation and device-approval links are built from ENVI_WEB_URL. A deployed
 * instance left on localhost mails links that resolve to the recipient's own
 * machine, which is silent and wastes the invitation.
 *
 * The Go server refuses to boot on this. A Worker has no boot step, so the
 * equivalent is refusing the requests that would produce an unusable link.
 */
export function assertLinkableWebUrl(env: Env) {
  if (isDeployed(env.ENVIRONMENT) && isLoopback(env.ENVI_WEB_URL)) {
    throw new ApiError(
      500,
      "misconfigured",
      `ENVI_WEB_URL is ${env.ENVI_WEB_URL} but ENVIRONMENT is ${env.ENVIRONMENT}; set it to the public dashboard URL`,
    );
  }
  if (isLoopback(env.ENVI_WEB_URL)) {
    console.warn(`ENVI_WEB_URL is ${env.ENVI_WEB_URL} — links will only work on this machine`);
  }
}
