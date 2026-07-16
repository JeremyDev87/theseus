import assert from "node:assert/strict";
import test from "node:test";

import { createReport } from "../src/report.js";
import type { VerificationResult } from "../src/types.js";

const artifact = { packageName: "fixture", packageVersion: "1.0.0", filename: "fixture.tgz", sha256: "a".repeat(64), size: 1 };

function result(overrides: Partial<VerificationResult> = {}): VerificationResult {
  return {
    artifact,
    receipts: [],
    incomplete: [],
    comparison: { findings: [], allowedDifferences: [] },
    ...overrides,
  };
}

test("report exit semantics are stable", () => {
  assert.equal(createReport("fixture", result()).exitCode, 0);
  assert.equal(
    createReport(
      "fixture",
      result({
        comparison: {
          allowedDifferences: [],
          findings: [
            {
              code: "THS-PARITY-001",
              probe: "help",
              field: "exit",
              profiles: ["a", "b"],
              message: "drift",
            },
          ],
        },
      }),
    ).exitCode,
    1,
  );
  assert.equal(
    createReport(
      "fixture",
      result({
        incomplete: [
          {
            code: "THS-INCOMPLETE-001",
            profile: "a",
            stage: "install",
            message: "failed",
          },
        ],
      }),
    ).exitCode,
    2,
  );
});
