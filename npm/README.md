# @shellhaki/envi-cli

The [Envi](https://envisecrets.com) command line tool: environment secrets,
encrypted, scoped and audited.

```bash
npm install -g @shellhaki/envi-cli
envi auth
```

Also available without Node:

```bash
curl -fsSL https://install.envisecrets.com | sh
```

The binary itself is a single static Go executable. This package installs the
build for your platform through npm's `optionalDependencies`, with no install
scripts, so it works under `npm ci --ignore-scripts` and pnpm's default
settings.

Full documentation: **[docs.envisecrets.com](https://docs.envisecrets.com)**
