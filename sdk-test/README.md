# sdk-test

A small Hono server exercising [`@shellhaki/envi-sdk`](https://www.npmjs.com/package/@shellhaki/envi-sdk).

```bash
bun install
bun run dev
```

Needs one credential in `.env` (Bun loads it automatically):

```
ENVI_TOKEN=st_...
# ENVI_API_URL=http://127.0.0.1:8080   # only if you self-host or run locally
```

Get a token with:

```bash
envi token create --name sdk-demo --permission write
```

## Try it

```bash
curl localhost:3000/                        # every value in the project
curl -X PUT localhost:3000/THEME -d dark    # set one
curl localhost:3000/THEME                   # read it back
curl -X DELETE localhost:3000/THEME         # remove it
curl localhost:3000/refresh                 # pick up changes made elsewhere
```

The store is fetched once on the first read and held in memory, so repeated
reads never touch the network. Writes go to the server and update what's held.
`/refresh` is how you see a change someone else made.
