import type {
  AllowedDelta,
  AllowedDifference,
  CompareField,
  ComparisonResult,
  Finding,
  JsonValueType,
  ProbeReceipt,
  RunReceipt,
  TheseusContract,
} from "./types.js";

export function compareReceipts(contract: TheseusContract, receipts: RunReceipt[]): ComparisonResult {
  const findings: Finding[] = [];
  const allowedDifferences: AllowedDifference[] = [];

  for (const receipt of receipts) {
    for (const probe of contract.probes) {
      const observed = receipt.probes.find((candidate) => candidate.id === probe.id);
      if (!observed) continue;
      if (probe.expect?.exit !== undefined && observed.exitCode !== probe.expect.exit) {
        findings.push({
          code: "THS-EXPECT-001",
          probe: probe.id,
          field: "exit",
          profiles: [receipt.profile],
          message: `${receipt.profile}/${probe.id}: expected exit ${probe.expect.exit}, observed ${String(observed.exitCode)}`,
          expected: probe.expect.exit,
          actual: observed.exitCode,
        });
      }
      if (probe.expect?.stdoutJson) {
        checkJsonExpectations(probe.id, receipt.profile, observed, probe.expect.stdoutJson, findings);
      }
    }
  }

  for (let leftIndex = 0; leftIndex < receipts.length; leftIndex += 1) {
    for (let rightIndex = leftIndex + 1; rightIndex < receipts.length; rightIndex += 1) {
      const left = receipts[leftIndex]!;
      const right = receipts[rightIndex]!;
      for (const probe of contract.probes) {
        const leftProbe = left.probes.find((candidate) => candidate.id === probe.id);
        const rightProbe = right.probes.find((candidate) => candidate.id === probe.id);
        if (!leftProbe || !rightProbe) continue;
        for (const field of probe.compare) {
          const values = fieldValues(field, left, right, leftProbe, rightProbe);
          if (Object.is(values.left, values.right)) continue;
          const allowed = contract.allowedDeltas.find((delta) => matchesAllowed(delta, probe.id, field, left.profile, right.profile, values.left, values.right));
          if (allowed) {
            allowedDifferences.push({ probe: probe.id, field, profiles: [left.profile, right.profile], reason: allowed.reason });
            continue;
          }
          findings.push(findingFor(field, probe.id, left.profile, right.profile, values));
        }
      }
    }
  }

  return { findings, allowedDifferences };
}

function fieldValues(
  field: CompareField,
  left: RunReceipt,
  right: RunReceipt,
  leftProbe: ProbeReceipt,
  rightProbe: ProbeReceipt,
): { left: number | string | null; right: number | string | null; displayLeft: unknown; displayRight: unknown } {
  if (field === "exit") {
    return { left: leftProbe.exitCode, right: rightProbe.exitCode, displayLeft: leftProbe.exitCode, displayRight: rightProbe.exitCode };
  }
  if (field === "stdout") {
    return { left: leftProbe.stdoutSha256, right: rightProbe.stdoutSha256, displayLeft: leftProbe.stdout, displayRight: rightProbe.stdout };
  }
  if (field === "stderr") {
    return { left: leftProbe.stderrSha256, right: rightProbe.stderrSha256, displayLeft: leftProbe.stderr, displayRight: rightProbe.stderr };
  }
  return { left: left.runtime.sha256, right: right.runtime.sha256, displayLeft: left.runtime.canonical, displayRight: right.runtime.canonical };
}

function matchesAllowed(
  delta: AllowedDelta,
  probe: string,
  field: CompareField,
  leftProfile: string,
  rightProfile: string,
  left: number | string | null,
  right: number | string | null,
): boolean {
  if (delta.probe !== probe || delta.field !== field) return false;
  if (delta.between[0] === leftProfile && delta.between[1] === rightProfile) {
    return field === "exit" ? Object.is(delta.from, left) && Object.is(delta.to, right) : delta.fromSha256 === left && delta.toSha256 === right;
  }
  if (delta.between[0] === rightProfile && delta.between[1] === leftProfile) {
    return field === "exit" ? Object.is(delta.from, right) && Object.is(delta.to, left) : delta.fromSha256 === right && delta.toSha256 === left;
  }
  return false;
}

function findingFor(
  field: CompareField,
  probe: string,
  leftProfile: string,
  rightProfile: string,
  values: { left: number | string | null; right: number | string | null; displayLeft: unknown; displayRight: unknown },
): Finding {
  const profiles = [leftProfile, rightProfile];
  if (field === "exit") {
    return {
      code: "THS-PARITY-001",
      probe,
      field,
      profiles,
      message: `${probe}: exit differs between ${leftProfile} and ${rightProfile}`,
      actual: { [leftProfile]: values.displayLeft, [rightProfile]: values.displayRight },
    };
  }
  const code = field === "runtime" ? "THS-RUNTIME-001" : "THS-PARITY-002";
  return {
    code,
    probe,
    field,
    profiles,
    message: `${probe}: ${field} differs between ${leftProfile} and ${rightProfile}`,
    actual: { [leftProfile]: values.displayLeft, [rightProfile]: values.displayRight },
    digests: { [leftProfile]: String(values.left), [rightProfile]: String(values.right) },
  };
}

function checkJsonExpectations(
  probe: string,
  profile: string,
  receipt: ProbeReceipt,
  expectations: Array<{ path: string; type: JsonValueType }>,
  findings: Finding[],
): void {
  let parsed: unknown;
  try {
    parsed = JSON.parse(receipt.stdout);
  } catch {
    findings.push({
      code: "THS-EXPECT-002",
      probe,
      field: "stdoutJson",
      profiles: [profile],
      message: `${profile}/${probe}: stdout is not valid JSON`,
    });
    return;
  }
  for (const expectation of expectations) {
    const value = getPath(parsed, expectation.path);
    const actualType = jsonType(value);
    if (actualType !== expectation.type) {
      findings.push({
        code: "THS-EXPECT-002",
        probe,
        field: "stdoutJson",
        profiles: [profile],
        message: `${profile}/${probe}: JSON path ${expectation.path} expected ${expectation.type}, observed ${actualType}`,
        expected: expectation.type,
        actual: actualType,
      });
    }
  }
}

function getPath(value: unknown, path: string): unknown {
  const segments = path.startsWith("/") ? parseJsonPointer(path) : path.split(".");
  if (!segments) return undefined;
  let current = value;
  for (const segment of segments) {
    if (current === null || typeof current !== "object" || !Object.hasOwn(current, segment)) return undefined;
    current = (current as Record<string, unknown>)[segment];
  }
  return current;
}

function parseJsonPointer(path: string): string[] | null {
  const segments: string[] = [];
  for (const token of path.slice(1).split("/")) {
    if (/~(?:[^01]|$)/.test(token)) return null;
    segments.push(token.replaceAll("~1", "/").replaceAll("~0", "~"));
  }
  return segments;
}

function jsonType(value: unknown): JsonValueType | "undefined" {
  if (value === undefined) return "undefined";
  if (value === null) return "null";
  if (Array.isArray(value)) return "array";
  return typeof value as JsonValueType;
}
