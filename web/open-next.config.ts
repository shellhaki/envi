import { defineCloudflareConfig } from "@opennextjs/cloudflare";

// Every page reads the visitor's session, so nothing is cached between requests.
export default defineCloudflareConfig();
