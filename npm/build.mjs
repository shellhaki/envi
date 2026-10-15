// Builds the npm packages for the CLI.
//
//   node npm/build.mjs <version>
//
// Produces npm/dist/:
//
//   envi-cli-<platform>/   one per target, each holding a single Go binary
//   envi-cli/              the wrapper everyone installs, whose
//                          optionalDependencies point at the seven above
//
// Versions are written here from the tag rather than committed, so nine
// package.json files can never drift out of step with each other.
//
// Publish the platform packages BEFORE the wrapper: the wrapper depends on
// exact versions of them, and npm resolves those at install time.

import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = join(root, "npm", "dist");

const version = (process.argv[2] ?? "").replace(/^v/, "");
if (!/^\d+\.\d+\.\d+/.test(version)) {
  console.error("usage: node npm/build.mjs <version>   (e.g. 0.4.0 or v0.4.0)");
  process.exit(2);
}

// Matches the targets in .goreleaser.yaml. `npmOs`/`npmCpu` are the values npm
// itself matches against, which differ from Go's names.
const TARGETS = [
  { goos: "darwin", goarch: "arm64", npmOs: "darwin", npmCpu: "arm64" },
  { goos: "darwin", goarch: "amd64", npmOs: "darwin", npmCpu: "x64" },
  { goos: "linux", goarch: "arm64", npmOs: "linux", npmCpu: "arm64" },
  { goos: "linux", goarch: "amd64", npmOs: "linux", npmCpu: "x64" },
  { goos: "linux", goarch: "arm", goarm: "7", npmOs: "linux", npmCpu: "arm" },
  { goos: "windows", goarch: "arm64", npmOs: "win32", npmCpu: "arm64" },
  { goos: "windows", goarch: "amd64", npmOs: "win32", npmCpu: "x64" },
];

const REPO = {
  type: "git",
  url: "git+https://github.com/shellhaki/envi.git",
};

rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });

const platformPackages = [];

for (const target of TARGETS) {
  const suffix = `${target.npmOs}-${target.npmCpu}`;
  const name = `@shellhaki/envi-cli-${suffix}`;
  const dir = join(out, `envi-cli-${suffix}`);
  const binary = target.goos === "windows" ? "envi.exe" : "envi";
  mkdirSync(dir, { recursive: true });

  // CLI_LDFLAGS carries the build-time settings from .env.cli, exactly as the
  // GoReleaser build does, so an npm-installed CLI talks to the same API.
  const ldflags = `-s -w -X main.version=${version}${process.env.CLI_LDFLAGS ?? ""}`;
  execFileSync("go", ["build", "-trimpath", "-ldflags", ldflags, "-o", join(dir, binary), "./cmd/envi"], {
    cwd: root,
    stdio: "inherit",
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: target.goos,
      GOARCH: target.goarch,
      ...(target.goarm ? { GOARM: target.goarm } : {}),
    },
  });

  writeFileSync(
    join(dir, "package.json"),
    JSON.stringify(
      {
        name,
        version,
        description: `The envi CLI binary for ${target.npmOs} ${target.npmCpu}.`,
        license: "MIT",
        repository: REPO,
        homepage: "https://envisecrets.com",
        // npm reads these and skips the package entirely on other machines, so
        // installing pulls down one binary rather than seven.
        os: [target.npmOs],
        cpu: [target.npmCpu],
        files: [binary],
        publishConfig: { access: "public" },
      },
      null,
      2,
    ) + "\n",
  );
  writeFileSync(
    join(dir, "README.md"),
    `# ${name}\n\nThe \`envi\` binary for ${target.npmOs} ${target.npmCpu}.\n\n` +
      "Do not install this directly. Install [@shellhaki/envi-cli](https://www.npmjs.com/package/@shellhaki/envi-cli),\n" +
      "which pulls in the right binary for your machine.\n",
  );
  platformPackages.push({ name, dir });
}

// The wrapper. Every platform package is an *optional* dependency: npm installs
// the one that matches and silently skips the rest, which is what makes this
// work without a postinstall script.
const wrapperDir = join(out, "envi-cli");
mkdirSync(join(wrapperDir, "bin"), { recursive: true });
cpSync(join(root, "npm", "shim.cjs"), join(wrapperDir, "bin", "envi.cjs"));
cpSync(join(root, "npm", "README.md"), join(wrapperDir, "README.md"));

writeFileSync(
  join(wrapperDir, "package.json"),
  JSON.stringify(
    {
      name: "@shellhaki/envi-cli",
      version,
      description: "Envi CLI — environment secrets, encrypted, scoped and audited.",
      license: "MIT",
      repository: REPO,
      homepage: "https://envisecrets.com",
      keywords: ["envi", "secrets", "environment", "dotenv", "cli"],
      bin: { envi: "bin/envi.cjs" },
      files: ["bin"],
      engines: { node: ">=18" },
      optionalDependencies: Object.fromEntries(
        platformPackages.map((p) => [p.name, version]),
      ),
      publishConfig: { access: "public" },
    },
    null,
    2,
  ) + "\n",
);

console.log(`\nBuilt ${platformPackages.length + 1} packages at version ${version} in npm/dist:`);
for (const p of platformPackages) console.log(`  ${p.name}`);
console.log(`  @shellhaki/envi-cli   (publish this one last)`);
