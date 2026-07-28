#!/usr/bin/env node

import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import path from "node:path";
import process from "node:process";

const distributions = new Map([
  ["darwin-arm64", "@seamless-cors/darwin-arm64"],
  ["darwin-x64", "@seamless-cors/darwin-x64"],
  ["win32-arm64", "@seamless-cors/win32-arm64"],
  ["win32-x64", "@seamless-cors/win32-x64"],
]);

const target = `${process.platform}-${process.arch}`;
const distribution = distributions.get(target);
if (distribution === undefined) {
  console.error(`seamless-cors does not support ${target}.`);
  console.error(
    "Supported platforms: macOS (arm64, x64) and Windows (arm64, x64).",
  );
  process.exit(1);
}

const require = createRequire(import.meta.url);
let distributionRoot;
try {
  distributionRoot = path.dirname(
    require.resolve(`${distribution}/package.json`),
  );
} catch {
  console.error(
    `The seamless-cors Gateway Distribution for ${target} is missing.`,
  );
  console.error(
    "Reinstall with optional dependencies enabled: npm install --global seamless-cors",
  );
  process.exit(1);
}

const executable = path.join(
  distributionRoot,
  "bin",
  process.platform === "win32" ? "seamless-cors.exe" : "seamless-cors",
);
const child = spawn(executable, process.argv.slice(2), {
  stdio: "inherit",
  windowsHide: false,
});

const signalHandlers = new Map();
const forwardedSignals = ["SIGINT", "SIGTERM", "SIGHUP"];
for (const signal of forwardedSignals) {
  const forwardSignal = () => {
    if (!child.killed) {
      child.kill(signal);
    }
  };
  signalHandlers.set(signal, forwardSignal);
  process.once(signal, forwardSignal);
}

child.once("error", (error) => {
  console.error(`Unable to start the seamless-cors Gateway Distribution: ${error.message}`);
  process.exitCode = 1;
});

child.once("exit", (code, signal) => {
  if (signal !== null) {
    for (const [forwardedSignal, handler] of signalHandlers) {
      process.removeListener(forwardedSignal, handler);
    }
    try {
      process.kill(process.pid, signal);
    } catch {
      process.exitCode = 1;
    }
    return;
  }
  process.exitCode = code ?? 1;
});
