import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { parseContract } from "../src/contract.js";
import { createReport } from "../src/report.js";
import { verifyTarget } from "../src/runner.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const fixture = path.resolve(here, "../../test/fixtures/parity-package");

test("a timed-out probe is incomplete and exits 2", { timeout: 60_000 }, async () => {
  const contract = parseContract({
    version: 1,
    profiles: {
      default: { installArgs: [] },
      noOptional: { installArgs: ["--omit=optional"] },
    },
    probes: [
      {
        id: "hang",
        argv: ["--hang"],
        timeoutMs: 100,
        compare: ["exit", "stdout", "stderr"],
      },
    ],
  });
  const result = await verifyTarget(fixture, contract);
  const report = createReport(fixture, result);
  assert.equal(result.incomplete.length, 2);
  assert.ok(result.incomplete.every((evidence) => evidence.stage === "probe" && evidence.probe === "hang"));
  assert.equal(report.status, "incomplete");
  assert.equal(report.exitCode, 2);
});
