import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";

import { compareReceipts } from "./compare.js";
import { sha256, sha256File, stableStringify } from "./hash.js";
import type {
  ArtifactReceipt,
  EnvironmentReceipt,
  IncompleteEvidence,
  NormalizerConfig,
  ProbeConfig,
  ProbeReceipt,
  RunReceipt,
  RuntimeIdentity,
  TheseusContract,
  VerificationResult,
} from "./types.js";

interface CommandResult {
  exitCode: number | null;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  durationMs: number;
  spawnError?: string;
}

interface PreparedArtifact extends ArtifactReceipt {
  tarballPath: string;
}

export async function verifyTarget(target: string, contract: TheseusContract): Promise<VerificationResult> {
  const tempRoot = await fs.mkdtemp(path.join(os.tmpdir(), "theseus-"));
  const receipts: RunReceipt[] = [];
  const incomplete: IncompleteEvidence[] = [];
  try {
    const npmVersionResult = await runCommand("npm", ["--version"], process.cwd(), 10_000);
    if (npmVersionResult.exitCode !== 0) throw new Error(`unable to resolve npm version: ${npmVersionResult.stderr}`);
    const environment: EnvironmentReceipt = {
      platform: process.platform,
      arch: process.arch,
      node: process.version,
      npm: npmVersionResult.stdout.trim(),
    };
    const artifact = await prepareArtifact(target, tempRoot);

    if (contract.source) {
      const sourceResult = await runSource(contract, environment);
      if (sourceResult.receipt) receipts.push(sourceResult.receipt);
      incomplete.push(...sourceResult.incomplete);
    }

    for (const [profileName, profile] of Object.entries(contract.profiles)) {
      const profileResult = await runInstalledProfile(profileName, profile.installArgs, artifact, contract, environment, tempRoot);
      if (profileResult.receipt) receipts.push(profileResult.receipt);
      incomplete.push(...profileResult.incomplete);
    }

    return {
      artifact: stripTarballPath(artifact),
      receipts,
      incomplete,
      comparison: compareReceipts(contract, receipts),
    };
  } finally {
    await fs.rm(tempRoot, { recursive: true, force: true });
  }
}

async function prepareArtifact(target: string, tempRoot: string): Promise<PreparedArtifact> {
  const artifactDir = path.join(tempRoot, "artifact");
  await fs.mkdir(artifactDir, { recursive: true });
  const resolvedTarget = target.startsWith(".") || target.startsWith("/") ? path.resolve(target) : target;
  const packed = await runCommand("npm", ["pack", resolvedTarget, "--json", "--pack-destination", artifactDir], process.cwd(), 120_000);
  if (packed.exitCode !== 0 || packed.timedOut || packed.spawnError) {
    const safeStderr = await sanitizeDiagnostic(packed.stderr, tempRoot);
    throw new Error(`npm pack failed: ${packed.spawnError ?? safeStderr.trim() ?? `exit ${String(packed.exitCode)}`}`);
  }
  let entries: unknown;
  try {
    entries = JSON.parse(packed.stdout);
  } catch {
    throw new Error(`npm pack did not return valid JSON: ${packed.stdout.slice(0, 400)}`);
  }
  if (!Array.isArray(entries) || entries.length !== 1) throw new Error("npm pack must return exactly one artifact");
  const entry = entries[0] as Record<string, unknown>;
  if (typeof entry.filename !== "string" || typeof entry.name !== "string" || typeof entry.version !== "string") {
    throw new Error("npm pack JSON is missing filename, name, or version");
  }
  const tarballPath = path.join(artifactDir, entry.filename);
  const stat = await fs.stat(tarballPath);
  return {
    packageName: entry.name,
    packageVersion: entry.version,
    filename: entry.filename,
    sha256: await sha256File(tarballPath),
    size: stat.size,
    tarballPath,
  };
}

async function runSource(
  contract: TheseusContract,
  environment: EnvironmentReceipt,
): Promise<{ receipt?: RunReceipt; incomplete: IncompleteEvidence[] }> {
  const source = contract.source!;
  const incomplete: IncompleteEvidence[] = [];
  const runtimeBase = {
    kind: "source" as const,
    binName: path.basename(source.command),
    executablePath: source.command,
    executableRealPath: source.command,
    optionalDependencies: [],
  };
  const canonical = stableStringify(runtimeBase);
  const runtime: RuntimeIdentity = { ...runtimeBase, canonical, sha256: sha256(canonical) };
  const sourceCwd = path.resolve(source.cwd);
  const sourceRealPath = await fs.realpath(sourceCwd);
  const probes = await runProbes(
    source.command,
    source.args,
    sourceCwd,
    contract.probes,
    contract.normalizers,
    "source",
    incomplete,
    [sourceCwd, sourceRealPath],
  );
  return {
    receipt: { subject: "source", profile: "source", installArgs: [], environment, runtime, probes },
    incomplete,
  };
}

async function runInstalledProfile(
  profileName: string,
  installArgs: string[],
  artifact: PreparedArtifact,
  contract: TheseusContract,
  environment: EnvironmentReceipt,
  tempRoot: string,
): Promise<{ receipt?: RunReceipt; incomplete: IncompleteEvidence[] }> {
  const incomplete: IncompleteEvidence[] = [];
  const projectDir = path.join(tempRoot, `profile-${safeName(profileName)}`);
  await fs.mkdir(projectDir, { recursive: true });
  await fs.writeFile(path.join(projectDir, "package.json"), JSON.stringify({ name: `theseus-consumer-${safeName(profileName)}`, private: true }), "utf8");
  const install = await runCommand(
    "npm",
    ["install", "--no-audit", "--no-fund", "--loglevel=error", ...installArgs, artifact.tarballPath],
    projectDir,
    180_000,
  );
  if (install.exitCode !== 0 || install.timedOut || install.spawnError) {
    const safeStderr = await sanitizeDiagnostic(install.stderr, tempRoot);
    incomplete.push({
      code: "THS-INCOMPLETE-001",
      profile: profileName,
      stage: "install",
      message: install.timedOut ? "npm install timed out" : install.spawnError ?? `npm install exited ${String(install.exitCode)}`,
      stderr: safeStderr,
    });
    return { incomplete };
  }

  const packageRoot = packageDirectory(path.join(projectDir, "node_modules"), artifact.packageName);
  let packageJson: Record<string, unknown>;
  try {
    packageJson = JSON.parse(await fs.readFile(path.join(packageRoot, "package.json"), "utf8")) as Record<string, unknown>;
  } catch (error) {
    incomplete.push({
      code: "THS-INCOMPLETE-001",
      profile: profileName,
      stage: "resolve-bin",
      message: `installed package.json could not be read: ${error instanceof Error ? error.message : String(error)}`,
    });
    return { incomplete };
  }

  let binName: string;
  try {
    binName = resolveBinName(packageJson, artifact.packageName, contract.bin);
  } catch (error) {
    incomplete.push({
      code: "THS-INCOMPLETE-001",
      profile: profileName,
      stage: "resolve-bin",
      message: error instanceof Error ? error.message : String(error),
    });
    return { incomplete };
  }
  const binFile = process.platform === "win32" ? `${binName}.cmd` : binName;
  const executable = path.join(projectDir, "node_modules", ".bin", binFile);
  let realExecutable: string;
  try {
    realExecutable = await fs.realpath(executable);
  } catch (error) {
    incomplete.push({
      code: "THS-INCOMPLETE-001",
      profile: profileName,
      stage: "resolve-bin",
      message: `bin ${binName} is missing: ${error instanceof Error ? error.message : String(error)}`,
    });
    return { incomplete };
  }

  const optionalDependencies = await installedOptionalDependencies(projectDir, packageJson);
  const projectRealPath = await fs.realpath(projectDir);
  const runtimeBase = {
    kind: "installed" as const,
    packageName: artifact.packageName,
    packageVersion: artifact.packageVersion,
    binName,
    executablePath: relative(projectDir, executable),
    executableRealPath: relative(projectRealPath, realExecutable),
    optionalDependencies,
  };
  const canonical = stableStringify(runtimeBase);
  const runtime: RuntimeIdentity = { ...runtimeBase, canonical, sha256: sha256(canonical) };
  const probes = await runProbes(
    executable,
    [],
    projectDir,
    contract.probes,
    contract.normalizers,
    profileName,
    incomplete,
    [projectDir, projectRealPath],
  );
  return {
    receipt: { subject: "installed", profile: profileName, installArgs, environment, runtime, probes },
    incomplete,
  };
}

async function runProbes(
  command: string,
  prefixArgs: string[],
  cwd: string,
  probes: ProbeConfig[],
  normalizers: NormalizerConfig[],
  profile: string,
  incomplete: IncompleteEvidence[],
  automaticRoots: string[],
): Promise<ProbeReceipt[]> {
  const receipts: ProbeReceipt[] = [];
  for (const probe of probes) {
    const result = await runCommand(command, [...prefixArgs, ...probe.argv], cwd, probe.timeoutMs);
    const stdout = normalize(result.stdout, "stdout", probe.id, normalizers, automaticRoots);
    const stderr = normalize(result.stderr, "stderr", probe.id, normalizers, automaticRoots);
    receipts.push({
      id: probe.id,
      argv: probe.argv,
      exitCode: result.exitCode,
      signal: result.signal,
      timedOut: result.timedOut,
      stdout,
      stderr,
      stdoutSha256: sha256(stdout),
      stderrSha256: sha256(stderr),
      durationMs: result.durationMs,
    });
    if (result.timedOut || result.spawnError) {
      const evidence: IncompleteEvidence = {
        code: "THS-INCOMPLETE-001",
        profile,
        stage: "probe",
        probe: probe.id,
        message: result.timedOut ? `probe timed out after ${probe.timeoutMs}ms` : result.spawnError!,
      };
      if (stderr) evidence.stderr = stderr;
      incomplete.push(evidence);
    }
  }
  return receipts;
}

function normalize(
  value: string,
  field: "stdout" | "stderr",
  probe: string,
  normalizers: NormalizerConfig[],
  automaticRoots: string[],
): string {
  let normalized = value.replaceAll("\r\n", "\n");
  for (const root of [...automaticRoots].sort((left, right) => right.length - left.length)) {
    normalized = normalized.replaceAll(root, "<subject-root>");
  }
  for (const config of normalizers) {
    if (config.probe && config.probe !== probe) continue;
    if (!config.fields.includes(field)) continue;
    normalized = normalized.replace(new RegExp(config.pattern, config.flags), config.replacement);
  }
  return normalized;
}

async function sanitizeDiagnostic(value: string, tempRoot: string): Promise<string> {
  let sanitized = value.replaceAll("\r\n", "\n");
  const realTempRoot = await fs.realpath(tempRoot);
  for (const root of [tempRoot, realTempRoot].sort((left, right) => right.length - left.length)) {
    sanitized = sanitized.replaceAll(root, "<theseus-temp>");
  }
  return sanitized.replace(/(?:\/[^\s]+)?\/\.npm\/_logs\/[^\s]+/g, "<npm-log>");
}

function resolveBinName(packageJson: Record<string, unknown>, packageName: string, requested?: string): string {
  const bin = packageJson.bin;
  let entries: Array<[string, string]>;
  if (typeof bin === "string") {
    entries = [[packageName.split("/").at(-1)!, bin]];
  } else if (bin !== null && typeof bin === "object" && !Array.isArray(bin)) {
    entries = Object.entries(bin as Record<string, unknown>).filter((entry): entry is [string, string] => typeof entry[1] === "string");
  } else {
    entries = [];
  }
  if (requested) {
    if (!entries.some(([name]) => name === requested)) throw new Error(`requested bin ${requested} is not declared by ${packageName}`);
    return requested;
  }
  if (entries.length !== 1) throw new Error(`${packageName} declares ${entries.length} bins; contract.bin is required`);
  return entries[0]![0];
}

async function installedOptionalDependencies(projectDir: string, packageJson: Record<string, unknown>): Promise<Array<{ name: string; version: string }>> {
  const raw = packageJson.optionalDependencies;
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return [];
  const results: Array<{ name: string; version: string }> = [];
  for (const name of Object.keys(raw as Record<string, unknown>).sort()) {
    try {
      const manifest = JSON.parse(await fs.readFile(path.join(packageDirectory(path.join(projectDir, "node_modules"), name), "package.json"), "utf8")) as Record<string, unknown>;
      if (typeof manifest.version === "string") results.push({ name, version: manifest.version });
    } catch {
      // Absence is the evidence for an omitted or unsupported optional dependency.
    }
  }
  return results;
}

function packageDirectory(nodeModules: string, name: string): string {
  return path.join(nodeModules, ...name.split("/"));
}

function relative(root: string, target: string): string {
  return path.relative(root, target).split(path.sep).join("/");
}

function safeName(value: string): string {
  return value.replace(/[^A-Za-z0-9_-]/g, "-");
}

function stripTarballPath(artifact: PreparedArtifact): ArtifactReceipt {
  const { tarballPath: _tarballPath, ...receipt } = artifact;
  return receipt;
}

function runCommand(command: string, args: string[], cwd: string, timeoutMs: number): Promise<CommandResult> {
  return new Promise((resolve) => {
    const started = Date.now();
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    let spawnError: string | undefined;
    const detached = process.platform !== "win32";
    const child = spawn(command, args, {
      cwd,
      env: process.env,
      detached,
      shell: process.platform === "win32",
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => (stdout += chunk));
    child.stderr.on("data", (chunk: string) => (stderr += chunk));
    child.on("error", (error) => {
      spawnError = error.message;
    });
    const timer = setTimeout(() => {
      timedOut = true;
      if (detached && child.pid !== undefined) {
        try {
          process.kill(-child.pid, "SIGKILL");
        } catch {
          child.kill("SIGKILL");
        }
      } else {
        child.kill("SIGKILL");
      }
    }, timeoutMs);
    child.on("close", (exitCode, signal) => {
      clearTimeout(timer);
      const result: CommandResult = {
        exitCode,
        signal,
        stdout,
        stderr,
        timedOut,
        durationMs: Date.now() - started,
      };
      if (spawnError) result.spawnError = spawnError;
      resolve(result);
    });
  });
}
