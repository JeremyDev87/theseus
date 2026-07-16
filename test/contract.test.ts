import assert from "node:assert/strict";
import test from "node:test";

import { ContractError, parseContract } from "../src/contract.js";

const valid = {
  version: 1,
  profiles: {
    default: { installArgs: [] },
    noOptional: { installArgs: ["--omit=optional"] },
  },
  probes: [
    {
      id: "help",
      argv: ["--help"],
      timeoutMs: 5_000,
      compare: ["exit", "stdout", "stderr", "runtime"],
      expect: { exit: 0 },
    },
  ],
};

test("accepts a versioned fail-closed contract", () => {
  const contract = parseContract(valid);
  assert.equal(contract.version, 1);
  assert.deepEqual(Object.keys(contract.profiles), ["default", "noOptional"]);
  assert.equal(contract.probes[0]?.id, "help");
});

test("rejects unknown contract fields", () => {
  assert.throws(
    () => parseContract({ ...valid, silentlyIgnoreMe: true }),
    (error: unknown) => error instanceof ContractError && /unknown field/.test(error.message),
  );
});

test("rejects unknown nested fields", () => {
  const input = structuredClone(valid) as Record<string, unknown>;
  const profiles = input.profiles as Record<string, Record<string, unknown>>;
  profiles.default = { installArgs: [], fallback: "silent" };
  assert.throws(() => parseContract(input), /profiles\.default.*unknown field/);
});

test("requires exact-value or exact-digest allowed deltas", () => {
  assert.throws(
    () =>
      parseContract({
        ...valid,
        allowedDeltas: [
          {
            probe: "help",
            between: ["default", "noOptional"],
            field: "stdout",
            reason: "intentional fallback",
          },
        ],
      }),
    /fromSha256.*toSha256/,
  );
});

test("rejects invalid normalizer regexes", () => {
  assert.throws(
    () =>
      parseContract({
        ...valid,
        normalizers: [
          {
            id: "broken",
            fields: ["stdout"],
            pattern: "[",
            replacement: "",
          },
        ],
      }),
    /invalid regular expression/,
  );
});

test("reserves the source profile name for the source subject", () => {
  assert.throws(
    () =>
      parseContract({
        ...valid,
        source: { command: "node", args: ["bin.js"], cwd: "." },
        profiles: {
          source: { installArgs: [] },
          default: { installArgs: [] },
        },
      }),
    /profiles\.source is reserved/,
  );
});
