#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const platform = process.platform;
const arch = process.arch;
const extension = platform === "win32" ? ".exe" : "";
const vendor = path.join(__dirname, "..", "vendor");

const candidates = [
  path.join(vendor, `agy-auth-${platform}-${arch}${extension}`),
  path.join(vendor, `agy-auth-${platform}-${arch === "x64" ? "amd64" : arch}${extension}`),
];
const binary = candidates.find((candidate) => fs.existsSync(candidate));

if (!binary) {
  console.error(`agy-auth: no prebuilt binary for ${platform}/${arch}.`);
  console.error(`Expected one of:\n  ${candidates.join("\n  ")}`);
  console.error("Install from source with:");
  console.error("  go install github.com/fikrilal/agy-auth/cmd/agy-auth@latest");
  process.exit(1);
}

if (platform !== "win32") {
  try {
    fs.chmodSync(binary, 0o755);
  } catch {
    // Best effort; the binary may already be executable.
  }
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`agy-auth: ${result.error.message}`);
  process.exit(1);
}
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status === null ? 1 : result.status);
