#!/usr/bin/env -S nub

import { chmod, copyFile, cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const registry = "https://registry.npmjs.org/";
const repository = {
  type: "git",
  url: "git+https://github.com/QzCurious/seamless-cors.git",
  directory: "packaging/npm",
};

const targets = [
  {
    goos: "darwin",
    goarch: "arm64",
    npmOS: "darwin",
    npmCPU: "arm64",
    packageName: "@seamless-cors/darwin-arm64",
    directory: "darwin-arm64",
    executable: "seamless-cors",
  },
  {
    goos: "darwin",
    goarch: "amd64",
    npmOS: "darwin",
    npmCPU: "x64",
    packageName: "@seamless-cors/darwin-x64",
    directory: "darwin-x64",
    executable: "seamless-cors",
  },
  {
    goos: "windows",
    goarch: "arm64",
    npmOS: "win32",
    npmCPU: "arm64",
    packageName: "@seamless-cors/win32-arm64",
    directory: "win32-arm64",
    executable: "seamless-cors.exe",
  },
  {
    goos: "windows",
    goarch: "amd64",
    npmOS: "win32",
    npmCPU: "x64",
    packageName: "@seamless-cors/win32-x64",
    directory: "win32-x64",
    executable: "seamless-cors.exe",
  },
] as const;

interface GoReleaserArtifact {
  type: string;
  path: string;
  goos: string;
  goarch: string;
  extra?: { ID?: string };
}

function releaseVersion(tag: string): { version: string; distTag: string } {
  const semver =
    /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?$/;
  if (!semver.test(tag)) {
    throw new Error(
      `Invalid release tag ${JSON.stringify(tag)}; expected vMAJOR.MINOR.PATCH with an optional prerelease.`,
    );
  }
  const version = tag.slice(1);
  return { version, distTag: version.includes("-") ? "next" : "latest" };
}

function runNub(args: string[], cwd: string) {
  const result = spawnSync("nub", args, { cwd, encoding: "utf8" });
  if (result.error !== undefined) {
    throw result.error;
  }
  return result;
}

function packageExists(packageName: string, version: string, cwd: string): boolean {
  const result = runNub(
    ["view", `${packageName}@${version}`, "version", "--registry", registry],
    cwd,
  );
  if (result.status === 0) {
    return true;
  }
  const output = `${result.stderr}\n${result.stdout}`.toLowerCase();
  if (
    output.includes("404") ||
    output.includes("not found") ||
    output.includes("no matching version")
  ) {
    return false;
  }
  throw new Error(
    result.stderr.trim() ||
      result.stdout.trim() ||
      `Unable to inspect ${packageName}@${version}.`,
  );
}

async function main(): Promise<void> {
  const [tag, artifactsArgument] = process.argv.slice(2);
  if (tag === undefined || artifactsArgument === undefined || process.argv.length !== 4) {
    throw new Error("Usage: publish.ts <git-tag> <goreleaser-artifacts.json>");
  }

  const { version, distTag } = releaseVersion(tag);
  const artifactsPath = path.resolve(artifactsArgument);
  const artifacts = JSON.parse(
    await readFile(artifactsPath, "utf8"),
  ) as GoReleaserArtifact[];
  const binaries = new Map<string, string>();

  for (const artifact of artifacts) {
    if (artifact.type !== "Binary" || artifact.extra?.ID !== "seamless-cors") {
      continue;
    }
    const target = `${artifact.goos}/${artifact.goarch}`;
    if (binaries.has(target)) {
      throw new Error(`GoReleaser reported duplicate binaries for ${target}.`);
    }
    binaries.set(target, path.resolve(process.cwd(), artifact.path));
  }

  const missing = targets
    .map(({ goos, goarch }) => `${goos}/${goarch}`)
    .filter((target) => !binaries.has(target));
  if (missing.length > 0) {
    throw new Error(`GoReleaser did not produce binaries for: ${missing.join(", ")}.`);
  }

  const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
  const repositoryRoot = path.resolve(scriptDirectory, "..", "..");
  const stagingRoot = await mkdtemp(path.join(tmpdir(), "seamless-cors-npm-"));

  try {
    const packages: Array<{ name: string; directory: string }> = [];

    for (const target of targets) {
      const packageRoot = path.join(stagingRoot, target.directory);
      const binRoot = path.join(packageRoot, "bin");
      const binary = path.join(binRoot, target.executable);
      await mkdir(binRoot, { recursive: true });
      await copyFile(binaries.get(`${target.goos}/${target.goarch}`)!, binary);
      await chmod(binary, 0o755);
      await writeFile(
        path.join(packageRoot, "package.json"),
        `${JSON.stringify(
          {
            name: target.packageName,
            version,
            description: `seamless-cors Gateway Distribution for ${target.npmOS}-${target.npmCPU}`,
            license: "MIT",
            repository,
            os: [target.npmOS],
            cpu: [target.npmCPU],
            files: ["bin/"],
            publishConfig: { access: "public", registry },
          },
          null,
          2,
        )}\n`,
      );
      await copyFile(
        path.join(repositoryRoot, "LICENSE"),
        path.join(packageRoot, "LICENSE"),
      );
      await copyFile(
        path.join(repositoryRoot, "README.md"),
        path.join(packageRoot, "README.md"),
      );
      packages.push({ name: target.packageName, directory: packageRoot });
    }

    const launcherRoot = path.join(stagingRoot, "seamless-cors");
    await cp(path.join(scriptDirectory, "launcher"), launcherRoot, { recursive: true });
    const launcherManifestPath = path.join(launcherRoot, "package.json");
    const launcherManifest = JSON.parse(
      await readFile(launcherManifestPath, "utf8"),
    ) as Record<string, unknown>;
    delete launcherManifest.private;
    launcherManifest.version = version;
    launcherManifest.optionalDependencies = Object.fromEntries(
      targets.map(({ packageName }) => [packageName, version]),
    );
    await writeFile(
      launcherManifestPath,
      `${JSON.stringify(launcherManifest, null, 2)}\n`,
    );
    await copyFile(
      path.join(repositoryRoot, "LICENSE"),
      path.join(launcherRoot, "LICENSE"),
    );
    await copyFile(
      path.join(repositoryRoot, "README.md"),
      path.join(launcherRoot, "README.md"),
    );
    packages.push({ name: "seamless-cors", directory: launcherRoot });

    for (const releasePackage of packages) {
      const pack = runNub(["pack", "--dry-run", "--json"], releasePackage.directory);
      if (pack.status !== 0) {
        throw new Error(
          pack.stderr.trim() ||
            pack.stdout.trim() ||
            `Unable to pack ${releasePackage.name}@${version}.`,
        );
      }

      if (packageExists(releasePackage.name, version, releasePackage.directory)) {
        process.stdout.write(
          `Verified existing ${releasePackage.name}@${version}; skipping publish.\n`,
        );
        continue;
      }

      const publish = runNub(
        [
          "publish",
          "--tag",
          distTag,
          "--access",
          "public",
          "--provenance",
          "--no-git-checks",
          "--registry",
          registry,
        ],
        releasePackage.directory,
      );
      if (publish.status !== 0) {
        throw new Error(
          publish.stderr.trim() ||
            publish.stdout.trim() ||
            `Unable to publish ${releasePackage.name}@${version}.`,
        );
      }
      process.stdout.write(
        `Published ${releasePackage.name}@${version} with tag ${distTag}.\n`,
      );
    }
  } finally {
    await rm(stagingRoot, { force: true, recursive: true });
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
