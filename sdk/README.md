# @shellhaki/envi-sdk

A key-value store for your [Envi](https://envisecrets.com) project, read and
written from code.

```bash
npm install @shellhaki/envi-sdk
```

```ts
import { Envi } from "@shellhaki/envi-sdk";

const envi = new Envi({ token: process.env.ENVI_TOKEN! });

await envi.set("THEME", "dark");
await envi.get("THEME"); // "dark"
```

This is not your secrets. Secrets belong to an environment, keep a version
history, and are pulled into a process by `envi run` or `envi pull`. These are
project-wide values your application owns and changes while it runs — a feature
flag, a cached setting, the timestamp of the last job. Both are encrypted at
rest with the same cipher.

Values load once and stay in memory, so reads cost nothing after the first.
Writes go to the server and update what's held.

Server-side only: the SDK refuses to construct in a browser, because a
credential in client code ships to every visitor along with everything it can
read.

Full documentation: **[docs.envisecrets.com/sdk](https://docs.envisecrets.com/sdk)**
