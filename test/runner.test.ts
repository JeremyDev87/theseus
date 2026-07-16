import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { parseContract } from "../src/contract.js";
import { verifyTarget } from "../src/runner.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixture = path.resolve(here, "../../test/fixtures/parity-package");

test("packs, installs, and compares a local CLI through two npm profiles", { timeout: 60_000 }, async () => {
  const contract = parseContract({
    version: 1,
    source: {
      command: "node",
      args: ["bin/parity-fixture.js"],
      cwd: fixture,
    },
    profiles: {
      default: { installArgs: [] },
      noOptional: { installArgs: ["--omit=optional"] },
    },
    probes: [
      {
        id: "help",
        argv: ["--help"],
        compare: ["exit", "stdout", "stderr"],
        expect: { exit: 0 },
      },
      {
        id: "cwd",
        argv: ["--cwd"],
        compare: ["exit", "stdout", "stderr"],
        expect: { exit: 0 },
      },
    ],
  });
  const result = await verifyTarget(fixture, contract);
  assert.equal(result.incomplete.length, 0);
  assert.equal(result.receipts.length, 3);
  assert.deepEqual(result.receipts.map((receipt) => receipt.profile), ["source", "default", "noOptional"]);
  assert.deepEqual(result.comparison.findings, []);
  assert.equal(result.artifact.packageName, "theseus-parity-fixture");
  assert.match(result.artifact.sha256, /^[a-f0-9]{64}$/);
});
