export type CompareField = "exit" | "stdout" | "stderr" | "runtime";
export type JsonValueType = "string" | "number" | "boolean" | "object" | "array" | "null";

export interface ProfileConfig {
  installArgs: string[];
}

export interface SourceConfig {
  command: string;
  args: string[];
  cwd: string;
}

export interface JsonPathExpectation {
  path: string;
  type: JsonValueType;
}

export interface ProbeExpectation {
  exit?: number;
  stdoutJson?: JsonPathExpectation[];
}

export interface ProbeConfig {
  id: string;
  argv: string[];
  timeoutMs: number;
  compare: CompareField[];
  expect?: ProbeExpectation;
}

export interface NormalizerConfig {
  id: string;
  probe?: string;
  fields: Array<"stdout" | "stderr">;
  pattern: string;
  flags: string;
  replacement: string;
}

export interface AllowedDelta {
  probe: string;
  between: [string, string];
  field: CompareField;
  reason: string;
  from?: number | null;
  to?: number | null;
  fromSha256?: string;
  toSha256?: string;
}

export interface TheseusContract {
  version: 1;
  bin?: string;
  source?: SourceConfig;
  profiles: Record<string, ProfileConfig>;
  probes: ProbeConfig[];
  normalizers: NormalizerConfig[];
  allowedDeltas: AllowedDelta[];
}

export interface ArtifactReceipt {
  packageName: string;
  packageVersion: string;
  filename: string;
  sha256: string;
  size: number;
}

export interface EnvironmentReceipt {
  platform: NodeJS.Platform;
  arch: string;
  node: string;
  npm: string;
}

export interface OptionalDependencyReceipt {
  name: string;
  version: string;
}

export interface RuntimeIdentity {
  kind: "source" | "installed";
  packageName?: string;
  packageVersion?: string;
  binName: string;
  executablePath: string;
  executableRealPath: string;
  optionalDependencies: OptionalDependencyReceipt[];
  canonical: string;
  sha256: string;
}

export interface ProbeReceipt {
  id: string;
  argv: string[];
  exitCode: number | null;
  signal: NodeJS.Signals | null;
  timedOut: boolean;
  stdout: string;
  stderr: string;
  stdoutSha256: string;
  stderrSha256: string;
  durationMs: number;
}

export interface RunReceipt {
  subject: "source" | "installed";
  profile: string;
  installArgs: string[];
  environment: EnvironmentReceipt;
  runtime: RuntimeIdentity;
  probes: ProbeReceipt[];
}

export interface IncompleteEvidence {
  code: "THS-INCOMPLETE-001";
  profile: string;
  stage: "pack" | "install" | "resolve-bin" | "probe";
  probe?: string;
  message: string;
  stderr?: string;
}

export interface Finding {
  code: "THS-PARITY-001" | "THS-PARITY-002" | "THS-RUNTIME-001" | "THS-EXPECT-001" | "THS-EXPECT-002";
  probe: string;
  field: CompareField | "stdoutJson";
  profiles: string[];
  message: string;
  expected?: unknown;
  actual?: unknown;
  digests?: Record<string, string>;
}

export interface AllowedDifference {
  probe: string;
  field: CompareField;
  profiles: [string, string];
  reason: string;
}

export interface ComparisonResult {
  findings: Finding[];
  allowedDifferences: AllowedDifference[];
}

export interface VerificationResult {
  artifact: ArtifactReceipt;
  receipts: RunReceipt[];
  incomplete: IncompleteEvidence[];
  comparison: ComparisonResult;
}

export interface VerificationReport extends VerificationResult {
  schemaVersion: 1;
  tool: { name: "theseus"; version: string };
  target: string;
  status: "pass" | "drift" | "incomplete";
  exitCode: 0 | 1 | 2;
}
