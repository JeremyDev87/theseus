import assert from "node:assert/strict";
import test from "node:test";

import { compareReceipts } from "../src/compare.js";
import { sha256 } from "../src/hash.js";
import type { RunReceipt, TheseusContract } from "../src/types.js";

function receipt(profile: string, values: { exit: number; stdout: string; stderr?: string; runtime?: string }): RunReceipt {
  const stdout = values.stdout;
  const stderr = values.stderr ?? "";
  const runtimeCanonical = values.runtime ?? "runtime-a";
  return {
    subject: "installed",
    profile,
    installArgs: profile === "noOptional" ? ["--omit=optional"] : [],
    environment: { platform: "darwin", arch: "arm64", node: "v24", npm: "11" },
    runtime: {
      kind: "installed",
      packageName: "fixture",
      packageVersion: "1.0.0",
      binName: "fixture",
      executablePath: "node_modules/.bin/fixture",
      executableRealPath: "node_modules/fixture/bin.js",
      optionalDependencies: [],
      canonical: runtimeCanonical,
      sha256: sha256(runtimeCanonical),
    },
    probes: [
      {
        id: "help",
        argv: ["--help"],
        exitCode: values.exit,
        signal: null,
        timedOut: false,
        stdout,
        stderr,
        stdoutSha256: sha256(stdout),
        stderrSha256: sha256(stderr),
        durationMs: 1,
      },
    ],
  };
}

const baseContract: TheseusContract = {
  version: 1,
  profiles: {
    default: { installArgs: [] },
    noOptional: { installArgs: ["--omit=optional"] },
  },
  probes: [{ id: "help", argv: ["--help"], timeoutMs: 5_000, compare: ["exit", "stdout", "stderr", "runtime"] }],
  normalizers: [],
  allowedDeltas: [],
};

test("reports exit, output, and runtime drift independently", () => {
  const result = compareReceipts(baseContract, [
    receipt("default", { exit: 0, stdout: "full\n", runtime: "native" }),
    receipt("noOptional", { exit: 1, stdout: "fallback\n", runtime: "fallback" }),
  ]);
  assert.deepEqual(
    result.findings.map((finding) => finding.code),
    ["THS-PARITY-001", "THS-PARITY-002", "THS-RUNTIME-001"],
  );
});

test("an exact allowed delta does not suppress unrelated drift", () => {
  const defaultReceipt = receipt("default", { exit: 0, stdout: "full\n", stderr: "", runtime: "native" });
  const fallbackReceipt = receipt("noOptional", {
    exit: 0,
    stdout: "fallback\n",
    stderr: "unexpected\n",
    runtime: "native",
  });
  const contract: TheseusContract = {
    ...baseContract,
    allowedDeltas: [
      {
        probe: "help",
        between: ["default", "noOptional"],
        field: "stdout",
        fromSha256: sha256("full\n"),
        toSha256: sha256("fallback\n"),
        reason: "documented reduced help",
      },
    ],
  };
  const result = compareReceipts(contract, [defaultReceipt, fallbackReceipt]);
  assert.equal(result.allowedDifferences.length, 1);
  assert.deepEqual(result.findings.map((finding) => finding.field), ["stderr"]);
});

test("checks declared exit expectations per profile", () => {
  const contract: TheseusContract = {
    ...baseContract,
    probes: [{ ...baseContract.probes[0]!, expect: { exit: 0 } }],
  };
  const result = compareReceipts(contract, [
    receipt("default", { exit: 0, stdout: "same" }),
    receipt("noOptional", { exit: 2, stdout: "same" }),
  ]);
  assert.ok(result.findings.some((finding) => finding.code === "THS-EXPECT-001"));
});

test("checks typed stdout JSON paths", () => {
  const contract: TheseusContract = {
    ...baseContract,
    probes: [
      {
        ...baseContract.probes[0]!,
        compare: ["exit"],
        expect: {
          stdoutJson: [
            { path: "meta.version", type: "string" },
            { path: "meta.ready", type: "boolean" },
          ],
        },
      },
    ],
  };
  const pass = compareReceipts(contract, [receipt("default", { exit: 0, stdout: '{"meta":{"version":"1","ready":true}}' })]);
  assert.deepEqual(pass.findings, []);

  const fail = compareReceipts(contract, [receipt("default", { exit: 0, stdout: '{"meta":{"version":1}}' })]);
  assert.deepEqual(fail.findings.map((finding) => finding.code), ["THS-EXPECT-002", "THS-EXPECT-002"]);
});
