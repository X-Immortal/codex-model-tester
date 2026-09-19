import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const desktopDir = path.resolve(scriptDir, "..");
const repoDir = path.resolve(desktopDir, "..");
const outputDir = path.join(desktopDir, "backend");
const temporaryDir = path.join(desktopDir, ".backend-tmp");
const requestedTarget = process.argv[2] || "host";

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { cwd: repoDir, stdio: "inherit", ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status || 1);
}

function build(goos, goarch, output) {
  run("go", ["build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", output, "./cmd/api"], {
    env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: goarch },
  });
}

fs.rmSync(outputDir, { recursive: true, force: true });
fs.rmSync(temporaryDir, { recursive: true, force: true });
fs.mkdirSync(outputDir, { recursive: true });

switch (requestedTarget) {
  case "host": {
    const name = process.platform === "win32" ? "Codex-Model-Tester.exe" : "Codex-Model-Tester";
    build(process.platform === "win32" ? "windows" : process.platform, process.arch, path.join(outputDir, name));
    break;
  }
  case "windows-x64":
    build("windows", "amd64", path.join(outputDir, "Codex-Model-Tester.exe"));
    break;
  case "macos-arm64":
    build("darwin", "arm64", path.join(outputDir, "Codex-Model-Tester"));
    break;
  case "macos-x64":
    build("darwin", "amd64", path.join(outputDir, "Codex-Model-Tester"));
    break;
  case "macos-universal": {
    if (os.platform() !== "darwin") {
      throw new Error("Universal macOS backend must be built on macOS because it requires lipo.");
    }
    fs.mkdirSync(temporaryDir, { recursive: true });
    const arm64 = path.join(temporaryDir, "backend-arm64");
    const x64 = path.join(temporaryDir, "backend-x64");
    build("darwin", "arm64", arm64);
    build("darwin", "amd64", x64);
    run("lipo", ["-create", arm64, x64, "-output", path.join(outputDir, "Codex-Model-Tester")]);
    fs.rmSync(temporaryDir, { recursive: true, force: true });
    break;
  }
  default:
    throw new Error(`Unknown backend target: ${requestedTarget}`);
}
