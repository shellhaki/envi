// Imports .sh and .ps1 files as text, the way wrangler's [[rules]] do in wrangler.toml.
Bun.plugin({
  name: "text-imports",
  setup(build) {
    build.onLoad({ filter: /\.(sh|ps1)$/ }, async ({ path }) => ({
      contents: `export default ${JSON.stringify(await Bun.file(path).text())};`,
      loader: "js",
    }));
  },
});
