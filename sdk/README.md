# @shellhaki/envi-sdk

Read your [Envi](https://envisecrets.com) secrets at runtime, from Node, Bun, or
any serverless platform.

```bash
npm install @shellhaki/envi-sdk
```

```ts
import { Envi } from "@shellhaki/envi-sdk";

const envi = new Envi({ token: process.env.ENVI_TOKEN! });
await envi.ready();

const databaseUrl = envi.require("DATABASE_URL");
```

Server-side only: the SDK refuses to construct in a browser, because a
credential in client code ships to every visitor along with every secret it can
read.

If you control the process, `envi run -- your-app` is simpler and needs no code
at all. Reach for this when you don't: Vercel, Netlify, Lambda, Cloudflare
Workers, or config that changes while the app runs.

Full documentation: **[docs.envisecrets.com/sdk](https://docs.envisecrets.com/sdk)**
