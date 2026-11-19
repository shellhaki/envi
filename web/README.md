# web

The Next.js app behind envisecrets.com (the self-hosting site), cloud.envisecrets.com (the Envi Cloud
dashboard) and docs.envisecrets.com (app/docs). `proxy.ts` sends each host to its pages.

```bash
bun run dev       # http://localhost:3000, docs at http://docs.localhost:3000
bun run preview   # the Cloudflare Worker build, run locally (needs .dev.vars)
bun run deploy    # build with OpenNext and deploy the envi-web Worker
```

It runs on Cloudflare Workers through [OpenNext](https://opennext.js.org/cloudflare); see wrangler.toml.
Self-hosted instances build the same app in Docker from `output: "standalone"`.
