import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, "../..");
const bin = path.join(root, "bin", "theseus.js");

function run(args: string[]) {
  return spawnSync(process.execPath, [bin, ...args], {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

test("CLI exposes help and version with exit 0", () => {
  const help = run(["--help"]);
  assert.equal(help.status, 0);
  assert.match(help.stdout, /Distribution parity tests for packaged CLIs/);
  assert.equal(help.stderr, "");

  const version = run(["--version"]);
  assert.equal(version.status, 0);
  assert.equal(version.stdout, "0.1.0\n");
  assert.equal(version.stderr, "");
});

test("CLI rejects unknown commands and options with exit 2", () => {
  const command = run(["wat"]);
  assert.equal(command.status, 2);
  assert.match(command.stderr, /unknown command wat/);

  const format = run(["verify", ".", "--format", "yaml"]);
  assert.equal(format.status, 2);
  assert.match(format.stderr, /--format must be text or json/);
});

test("CLI emits parseable JSON incomplete evidence for a missing contract", () => {
  const result = run([
    "verify",
    ".",
    "--contract",
    path.join(root, "test", "fixtures", "missing-contract.json"),
    "--format",
    "json",
  ]);
  assert.equal(result.status, 2);
  assert.equal(result.stderr, "");
  const report = JSON.parse(result.stdout) as Record<string, unknown>;
  assert.equal(report.status, "incomplete");
  assert.equal(report.exitCode, 2);
  assert.deepEqual((report.error as Record<string, unknown>).code, "THS-INCOMPLETE-001");
});
