import assert from "node:assert/strict";
import { chmod, cp, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";

const packagingRoot = path.dirname(fileURLToPath(import.meta.url));
const launcherSource = path.join(packagingRoot, "launcher");

async function installedLauncher(t, withDistribution = true) {
  const root = await mkdtemp(path.join(tmpdir(), "seamless-cors-launcher-"));
  t.after(() => rm(root, { force: true, recursive: true }));

  const launcherRoot = path.join(root, "node_modules", "seamless-cors");
  await cp(launcherSource, launcherRoot, { recursive: true });
  if (withDistribution) {
    const distribution = path.join(
      root,
      "node_modules",
      "@seamless-cors",
      "darwin-arm64",
    );
    await mkdir(path.join(distribution, "bin"), { recursive: true });
    await writeFile(
      path.join(distribution, "package.json"),
      JSON.stringify({ name: "@seamless-cors/darwin-arm64", version: "0.4.0" }),
    );
    const executable = path.join(distribution, "bin", "seamless-cors");
    await writeFile(
      executable,
      `#!/usr/bin/env node
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => { input += chunk; });
process.stdin.on("end", () => {
  process.stdout.write(JSON.stringify({ args: process.argv.slice(2), input }) + "\\n");
  process.stderr.write("gateway stderr\\n");
  process.exit(23);
});
`,
    );
    await chmod(executable, 0o755);
  }

  return {
    root,
    launcher: path.join(launcherRoot, "bin", "seamless-cors.js"),
  };
}

async function bootstrap(root, launcher, platform, arch, args = []) {
  const file = path.join(root, "bootstrap.mjs");
  await writeFile(
    file,
    `Object.defineProperty(process, "platform", { value: ${JSON.stringify(platform)} });
Object.defineProperty(process, "arch", { value: ${JSON.stringify(arch)} });
process.argv = [process.execPath, ${JSON.stringify(launcher)}, ...${JSON.stringify(args)}];
await import(${JSON.stringify(pathToFileURL(launcher).href)});
`,
  );
  return file;
}

test("runs the installed native distribution with inherited stdio", async (t) => {
  const context = await installedLauncher(t);
  const entry = await bootstrap(
    context.root,
    context.launcher,
    "darwin",
    "arm64",
    ["start", "--example"],
  );
  const result = spawnSync(process.execPath, [entry], {
    encoding: "utf8",
    input: "gateway stdin\n",
  });

  assert.equal(result.status, 23, result.stderr);
  assert.equal(
    result.stdout,
    '{"args":["start","--example"],"input":"gateway stdin\\n"}\n',
  );
  assert.equal(result.stderr, "gateway stderr\n");
});

test("explains unsupported platforms", async (t) => {
  const context = await installedLauncher(t, false);
  const entry = await bootstrap(context.root, context.launcher, "linux", "x64");
  const result = spawnSync(process.execPath, [entry], { encoding: "utf8" });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /does not support linux-x64/);
});

test("explains a missing optional dependency", async (t) => {
  const context = await installedLauncher(t, false);
  const entry = await bootstrap(context.root, context.launcher, "darwin", "arm64");
  const result = spawnSync(process.execPath, [entry], { encoding: "utf8" });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /Gateway Distribution for darwin-arm64 is missing/);
  assert.match(result.stderr, /npm install --global seamless-cors/);
});
