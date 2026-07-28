import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const packagingRoot = path.dirname(fileURLToPath(import.meta.url));
const publisher = path.join(packagingRoot, "publish.ts");
const packageNames = [
  "@seamless-cors/darwin-arm64",
  "@seamless-cors/darwin-x64",
  "@seamless-cors/win32-arm64",
  "@seamless-cors/win32-x64",
  "seamless-cors",
];

async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), "seamless-cors-publish-"));
  t.after(() => rm(root, { force: true, recursive: true }));

  const dist = path.join(root, "dist");
  const tools = path.join(root, "tools");
  const log = path.join(root, "nub.log");
  await mkdir(dist, { recursive: true });
  await mkdir(tools, { recursive: true });

  const targets = [
    ["darwin", "arm64", "seamless-cors"],
    ["darwin", "amd64", "seamless-cors"],
    ["windows", "arm64", "seamless-cors.exe"],
    ["windows", "amd64", "seamless-cors.exe"],
  ];
  const artifacts = [];
  for (const [goos, goarch, executable] of targets) {
    const binary = path.join(dist, `${goos}-${goarch}`, executable);
    await mkdir(path.dirname(binary), { recursive: true });
    await writeFile(binary, `binary:${goos}/${goarch}`);
    artifacts.push({
      type: "Binary",
      path: path.relative(root, binary),
      goos,
      goarch,
      extra: { ID: "seamless-cors" },
    });
  }
  const artifactsFile = path.join(dist, "artifacts.json");
  await writeFile(artifactsFile, JSON.stringify(artifacts));

  const fakeNub = path.join(tools, "nub");
  await writeFile(
    fakeNub,
    `#!/usr/bin/env node
const fs = require("node:fs");
const path = require("node:path");
const args = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync("package.json", "utf8"));
const existing = new Set((process.env.EXISTING_PACKAGES || "").split(",").filter(Boolean));
if (args[0] === "view") {
  if (existing.has(args[1])) process.exit(0);
  process.stderr.write("package not found\\n");
  process.exit(1);
}
const bin = manifest.name === "seamless-cors"
  ? "bin/seamless-cors.js"
  : path.join("bin", manifest.os[0] === "win32" ? "seamless-cors.exe" : "seamless-cors");
fs.appendFileSync(process.env.NUB_LOG, JSON.stringify({
  command: args[0],
  args,
  manifest,
  binary: fs.readFileSync(bin, "utf8"),
  mode: fs.statSync(bin).mode & 0o777,
}) + "\\n");
process.exit(0);
`,
  );
  await chmod(fakeNub, 0o755);

  return { root, artifactsFile, tools, log };
}

function runPublisher({ root, artifactsFile, tools, log }, tag, existing = "") {
  return spawnSync(process.execPath, [publisher, tag, artifactsFile], {
    cwd: root,
    encoding: "utf8",
    env: {
      ...process.env,
      EXISTING_PACKAGES: existing,
      NUB_LOG: log,
      PATH: `${tools}${path.delimiter}${process.env.PATH}`,
    },
  });
}

async function invocations(log) {
  return (await readFile(log, "utf8"))
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => JSON.parse(line));
}

test("generates and publishes native packages before the launcher", async (t) => {
  const context = await fixture(t);
  const result = runPublisher(context, "v0.4.0-beta.1");
  assert.equal(result.status, 0, result.stderr);

  const calls = await invocations(context.log);
  const publishes = calls.filter(({ command }) => command === "publish");
  assert.deepEqual(
    publishes.map(({ manifest }) => manifest.name),
    packageNames,
  );
  assert.equal(calls.filter(({ command }) => command === "pack").length, 5);

  for (const [index, call] of publishes.entries()) {
    assert.equal(call.manifest.version, "0.4.0-beta.1");
    assert.deepEqual(call.args, [
      "publish",
      "--tag",
      "next",
      "--access",
      "public",
      "--provenance",
      "--no-git-checks",
      "--registry",
      "https://registry.npmjs.org/",
    ]);
    if (index < 4) {
      assert.equal(call.mode, 0o755);
      assert.match(call.binary, /^binary:/);
    }
  }
  assert.deepEqual(publishes.at(-1).manifest.optionalDependencies, {
    "@seamless-cors/darwin-arm64": "0.4.0-beta.1",
    "@seamless-cors/darwin-x64": "0.4.0-beta.1",
    "@seamless-cors/win32-arm64": "0.4.0-beta.1",
    "@seamless-cors/win32-x64": "0.4.0-beta.1",
  });
});

test("a failed release can skip versions already published", async (t) => {
  const context = await fixture(t);
  const existing = [
    "@seamless-cors/darwin-arm64@0.4.0",
    "@seamless-cors/darwin-x64@0.4.0",
  ].join(",");
  const result = runPublisher(context, "v0.4.0", existing);
  assert.equal(result.status, 0, result.stderr);

  const calls = await invocations(context.log);
  assert.deepEqual(
    calls
      .filter(({ command }) => command === "publish")
      .map(({ manifest }) => manifest.name),
    packageNames.slice(2),
  );
});

test("rejects an incomplete GoReleaser build", async (t) => {
  const context = await fixture(t);
  const artifacts = JSON.parse(await readFile(context.artifactsFile, "utf8"));
  await writeFile(context.artifactsFile, JSON.stringify(artifacts.slice(1)));

  const result = runPublisher(context, "v0.4.0");
  assert.equal(result.status, 1);
  assert.match(result.stderr, /did not produce binaries for: darwin\/arm64/);
});
