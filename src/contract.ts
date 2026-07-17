import type {
  AllowedDelta,
  CompareField,
  JsonPathExpectation,
  JsonValueType,
  NormalizerConfig,
  ProbeConfig,
  ProbeExpectation,
  ProfileConfig,
  SourceConfig,
  TheseusContract,
} from "./types.js";

export class ContractError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ContractError";
  }
}

const compareFields = new Set<CompareField>(["exit", "stdout", "stderr", "runtime"]);
const jsonTypes = new Set<JsonValueType>(["string", "number", "boolean", "object", "array", "null"]);
const digestPattern = /^[a-f0-9]{64}$/;

export function parseContract(input: unknown): TheseusContract {
  const root = objectAt(input, "contract");
  keys(root, ["version", "bin", "source", "profiles", "probes", "normalizers", "allowedDeltas"], "contract");
  if (root.version !== 1) throw new ContractError("contract.version must be 1");

  const profilesObject = objectAt(root.profiles, "profiles");
  const profileEntries = Object.entries(profilesObject);
  if (profileEntries.length < 2) throw new ContractError("profiles must declare at least two installation profiles");
  const profiles: Record<string, ProfileConfig> = {};
  for (const [name, raw] of profileEntries) {
    if (!/^[A-Za-z][A-Za-z0-9_-]*$/.test(name)) throw new ContractError(`profiles.${name}: invalid profile name`);
    const profile = objectAt(raw, `profiles.${name}`);
    keys(profile, ["installArgs"], `profiles.${name}`);
    profiles[name] = { installArgs: stringArray(profile.installArgs ?? [], `profiles.${name}.installArgs`) };
  }

  if (!Array.isArray(root.probes) || root.probes.length === 0) throw new ContractError("probes must be a non-empty array");
  const probes = root.probes.map((raw, index) => parseProbe(raw, index));
  const probeIds = new Set<string>();
  for (const probe of probes) {
    if (probeIds.has(probe.id)) throw new ContractError(`duplicate probe id: ${probe.id}`);
    probeIds.add(probe.id);
  }

  const source = root.source === undefined ? undefined : parseSource(root.source);
  if (source && Object.hasOwn(profiles, "source")) {
    throw new ContractError("profiles.source is reserved when a source subject is declared");
  }
  const profileNames = new Set(Object.keys(profiles));
  if (source) profileNames.add("source");

  const normalizers = parseNormalizers(root.normalizers ?? [], probeIds);
  const allowedDeltas = parseAllowedDeltas(root.allowedDeltas ?? [], probeIds, profileNames);

  const contract: TheseusContract = {
    version: 1,
    profiles,
    probes,
    normalizers,
    allowedDeltas,
  };
  if (root.bin !== undefined) contract.bin = nonEmptyString(root.bin, "bin");
  if (source) contract.source = source;
  return contract;
}

function parseSource(raw: unknown): SourceConfig {
  const source = objectAt(raw, "source");
  keys(source, ["command", "args", "cwd"], "source");
  return {
    command: nonEmptyString(source.command, "source.command"),
    args: stringArray(source.args ?? [], "source.args"),
    cwd: source.cwd === undefined ? "." : nonEmptyString(source.cwd, "source.cwd"),
  };
}

function parseProbe(raw: unknown, index: number): ProbeConfig {
  const path = `probes[${index}]`;
  const probe = objectAt(raw, path);
  keys(probe, ["id", "argv", "timeoutMs", "compare", "expect"], path);
  const id = nonEmptyString(probe.id, `${path}.id`);
  const timeoutMs = probe.timeoutMs === undefined ? 10_000 : positiveInteger(probe.timeoutMs, `${path}.timeoutMs`);
  const rawCompare = probe.compare ?? ["exit", "stdout", "stderr"];
  const compare = stringArray(rawCompare, `${path}.compare`) as CompareField[];
  if (compare.length === 0 || compare.some((field) => !compareFields.has(field))) {
    throw new ContractError(`${path}.compare must contain only exit, stdout, stderr, or runtime`);
  }
  if (new Set(compare).size !== compare.length) throw new ContractError(`${path}.compare contains duplicates`);
  const result: ProbeConfig = {
    id,
    argv: stringArray(probe.argv ?? [], `${path}.argv`),
    timeoutMs,
    compare,
  };
  if (probe.expect !== undefined) result.expect = parseExpectation(probe.expect, `${path}.expect`);
  return result;
}

function parseExpectation(raw: unknown, path: string): ProbeExpectation {
  const expect = objectAt(raw, path);
  keys(expect, ["exit", "stdoutJson"], path);
  const result: ProbeExpectation = {};
  if (expect.exit !== undefined) result.exit = integer(expect.exit, `${path}.exit`);
  if (expect.stdoutJson !== undefined) {
    if (!Array.isArray(expect.stdoutJson) || expect.stdoutJson.length === 0) {
      throw new ContractError(`${path}.stdoutJson must be a non-empty array`);
    }
    result.stdoutJson = expect.stdoutJson.map((rawPath, index): JsonPathExpectation => {
      const itemPath = `${path}.stdoutJson[${index}]`;
      const item = objectAt(rawPath, itemPath);
      keys(item, ["path", "type"], itemPath);
      const type = nonEmptyString(item.type, `${itemPath}.type`) as JsonValueType;
      if (!jsonTypes.has(type)) throw new ContractError(`${itemPath}.type is invalid`);
      return { path: nonEmptyString(item.path, `${itemPath}.path`), type };
    });
  }
  if (result.exit === undefined && result.stdoutJson === undefined) throw new ContractError(`${path} must declare exit or stdoutJson`);
  return result;
}

function parseNormalizers(raw: unknown, probeIds: Set<string>): NormalizerConfig[] {
  if (!Array.isArray(raw)) throw new ContractError("normalizers must be an array");
  return raw.map((item, index) => {
    const path = `normalizers[${index}]`;
    const normalizer = objectAt(item, path);
    keys(normalizer, ["id", "probe", "fields", "pattern", "flags", "replacement"], path);
    const fields = stringArray(normalizer.fields, `${path}.fields`) as Array<"stdout" | "stderr">;
    if (fields.length === 0 || fields.some((field) => field !== "stdout" && field !== "stderr")) {
      throw new ContractError(`${path}.fields must contain stdout or stderr`);
    }
    const probe = normalizer.probe === undefined ? undefined : nonEmptyString(normalizer.probe, `${path}.probe`);
    if (probe && !probeIds.has(probe)) throw new ContractError(`${path}.probe references unknown probe ${probe}`);
    const pattern = string(normalizer.pattern, `${path}.pattern`);
    const flags = normalizer.flags === undefined ? "g" : string(normalizer.flags, `${path}.flags`);
    const replacement = string(normalizer.replacement, `${path}.replacement`);
    try {
      new RegExp(pattern, flags);
    } catch (error) {
      throw new ContractError(`${path}: invalid regular expression: ${error instanceof Error ? error.message : String(error)}`);
    }
    const result: NormalizerConfig = {
      id: nonEmptyString(normalizer.id, `${path}.id`),
      fields: [...new Set(fields)],
      pattern,
      flags,
      replacement,
    };
    if (probe) result.probe = probe;
    return result;
  });
}

function parseAllowedDeltas(raw: unknown, probeIds: Set<string>, profileNames: Set<string>): AllowedDelta[] {
  if (!Array.isArray(raw)) throw new ContractError("allowedDeltas must be an array");
  const seen = new Set<string>();
  return raw.map((item, index) => {
    const path = `allowedDeltas[${index}]`;
    const delta = objectAt(item, path);
    keys(delta, ["probe", "between", "field", "reason", "from", "to", "fromSha256", "toSha256"], path);
    const probe = nonEmptyString(delta.probe, `${path}.probe`);
    if (!probeIds.has(probe)) throw new ContractError(`${path}.probe references unknown probe ${probe}`);
    const betweenRaw = stringArray(delta.between, `${path}.between`);
    if (betweenRaw.length !== 2 || betweenRaw[0] === betweenRaw[1]) throw new ContractError(`${path}.between must contain two different profiles`);
    const between: [string, string] = [betweenRaw[0]!, betweenRaw[1]!];
    for (const profile of between) if (!profileNames.has(profile)) throw new ContractError(`${path}.between references unknown profile ${profile}`);
    const field = nonEmptyString(delta.field, `${path}.field`) as CompareField;
    if (!compareFields.has(field)) throw new ContractError(`${path}.field is invalid`);
    const result: AllowedDelta = {
      probe,
      between,
      field,
      reason: nonEmptyString(delta.reason, `${path}.reason`),
    };
    if (field === "exit") {
      if (!("from" in delta) || !("to" in delta)) throw new ContractError(`${path}: exit delta requires from and to`);
      result.from = nullableInteger(delta.from, `${path}.from`);
      result.to = nullableInteger(delta.to, `${path}.to`);
      if (delta.fromSha256 !== undefined || delta.toSha256 !== undefined) throw new ContractError(`${path}: exit delta cannot use digests`);
    } else {
      if (delta.fromSha256 === undefined || delta.toSha256 === undefined) {
        throw new ContractError(`${path}: non-exit delta requires fromSha256 and toSha256`);
      }
      const fromSha256 = nonEmptyString(delta.fromSha256, `${path}.fromSha256`);
      const toSha256 = nonEmptyString(delta.toSha256, `${path}.toSha256`);
      if (!digestPattern.test(fromSha256) || !digestPattern.test(toSha256)) throw new ContractError(`${path}: fromSha256 and toSha256 must be lowercase SHA-256 digests`);
      result.fromSha256 = fromSha256;
      result.toSha256 = toSha256;
      if ("from" in delta || "to" in delta) throw new ContractError(`${path}: non-exit delta must use fromSha256 and toSha256`);
    }
    const canonicalPair = [...between].sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
    const key = `${probe}\u0000${canonicalPair.join("\u0000")}\u0000${field}`;
    if (seen.has(key)) throw new ContractError(`${path}: duplicate allowed delta`);
    seen.add(key);
    return result;
  });
}

function objectAt(value: unknown, path: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw new ContractError(`${path} must be an object`);
  return value as Record<string, unknown>;
}

function keys(value: Record<string, unknown>, allowed: string[], path: string): void {
  const set = new Set(allowed);
  for (const key of Object.keys(value)) if (!set.has(key)) throw new ContractError(`${path}: unknown field ${key}`);
}

function string(value: unknown, path: string): string {
  if (typeof value !== "string") throw new ContractError(`${path} must be a string`);
  return value;
}

function nonEmptyString(value: unknown, path: string): string {
  const result = string(value, path);
  if (result.trim() === "") throw new ContractError(`${path} must not be empty`);
  return result;
}

function stringArray(value: unknown, path: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) throw new ContractError(`${path} must be an array of strings`);
  return [...value] as string[];
}

function integer(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isInteger(value)) throw new ContractError(`${path} must be an integer`);
  return value;
}

function positiveInteger(value: unknown, path: string): number {
  const result = integer(value, path);
  if (result <= 0) throw new ContractError(`${path} must be positive`);
  return result;
}

function nullableInteger(value: unknown, path: string): number | null {
  if (value === null) return null;
  return integer(value, path);
}
