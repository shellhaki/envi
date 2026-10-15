#!/usr/bin/env node
"use strict";

// The `envi` command installed by @shellhaki/envi-cli.
//
// The real CLI is a Go binary. npm installs exactly one of the per-platform
// packages listed in optionalDependencies — the one matching this machine — and
// this file hands over to it.
//
// There is deliberately no postinstall step: pnpm blocks those by default and
// `npm ci --ignore-scripts` skips them, which would leave a package that
// installs "successfully" and then has no working command.

const { spawnSync } = require("node:child_process");

const PLATFORMS = {
  "darwin-arm64": "@shellhaki/envi-cli-darwin-arm64",
  "darwin-x64": "@shellhaki/envi-cli-darwin-x64",
  "linux-arm64": "@shellhaki/envi-cli-linux-arm64",
  "linux-x64": "@shellhaki/envi-cli-linux-x64",
  "linux-arm": "@shellhaki/envi-cli-linux-arm",
  "win32-arm64": "@shellhaki/envi-cli-win32-arm64",
  "win32-x64": "@shellhaki/envi-cli-win32-x64",
};

function binaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORMS[key];
  if (!pkg) {
    fail(
      `Envi has no prebuilt binary for ${key}.`,
      "Install from source instead: https://docs.envisecrets.com/installation",
    );
  }
  const file = process.platform === "win32" ? "envi.exe" : "envi";
  try {
    return require.resolve(`${pkg}/${file}`);
  } catch {
    // The optional dependency is missing. Usually --no-optional, a lockfile
    // built on a different platform, or an interrupted install.
    fail(
      `Envi is installed but ${pkg} is not.`,
      "That package holds the binary for this machine. Reinstall with:",
      "  npm install -g @shellhaki/envi-cli",
    );
  }
}

function fail(...lines) {
  for (const line of lines) console.error(line);
  process.exit(1);
}

const result = spawnSync(binaryPath(), process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  fail(`Could not run envi: ${result.error.message}`);
}
// Report the status the way a shell would, so wrapping envi in npm changes
// nothing for scripts that check the exit code.
if (result.signal) {
  process.exit(128 + (require("node:os").constants.signals[result.signal] ?? 1));
}
process.exit(result.status ?? 1);
