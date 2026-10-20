#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const vendor = path.join(root, "npm", "vendor");
const packageJSON = JSON.parse(readFileSync(path.join(root, "package.json"), "utf8"));

const targets = [
  { goos: "linux", goarch: "amd64", npmOS: "linux", npmArch: "x64" },
  { goos: "linux", goarch: "arm64", npmOS: "linux", npmArch: "arm64" },
  { goos: "darwin", goarch: "amd64", npmOS: "darwin", npmArch: "x64" },
  { goos: "darwin", goarch: "arm64", npmOS: "darwin", npmArch: "arm64" },
  { goos: "windows", goarch: "amd64", npmOS: "win32", npmArch: "x64" },
  { goos: "windows", goarch: "arm64", npmOS: "win32", npmArch: "arm64" },
];

const hostOS = process.platform === "win32" ? "windows" : process.platform;
const hostArch = process.arch === "x64" ? "amd64" : process.arch;
const host = targets.find(
  (target) => target.goos === hostOS && target.goarch === hostArch,
);
const buildAll = process.argv.includes("--all");
const selected = buildAll ? targets : host ? [host] : [];

if (selected.length === 0) {
  console.error(`No prebuilt target for host ${process.platform}/${process.arch}.`);
  console.error("Supported targets: linux/darwin/windows on x64/arm64.");
  process.exit(1);
}

rmSync(vendor, { recursive: true, force: true });
mkdirSync(vendor, { recursive: true });

const ldflags = [
  "-s",
  "-w",
  `-X github.com/fikrilal/agy-auth/internal/cli.Version=${packageJSON.version}`,
].join(" ");

for (const target of selected) {
  const extension = target.goos === "windows" ? ".exe" : "";
  const output = path.join(
    vendor,
    `agy-auth-${target.npmOS}-${target.npmArch}${extension}`,
  );
  console.log(`building ${path.relative(root, output)}`);
  execFileSync(
    "go",
    ["build", "-trimpath", "-ldflags", ldflags, "-o", output, "./cmd/agy-auth"],
    {
      cwd: root,
      stdio: "inherit",
      env: {
        ...process.env,
        GOOS: target.goos,
        GOARCH: target.goarch,
        CGO_ENABLED: "0",
      },
    },
  );
  if (process.platform !== "win32") {
    chmodSync(output, 0o755);
  }
}

const checksums = selected.map((target) => {
  const extension = target.goos === "windows" ? ".exe" : "";
  const filename = `agy-auth-${target.npmOS}-${target.npmArch}${extension}`;
  const digest = createHash("sha256")
    .update(readFileSync(path.join(vendor, filename)))
    .digest("hex");
  return `${digest}  ${filename}`;
});
writeFileSync(path.join(vendor, "checksums.txt"), checksums.join("\n") + "\n");
