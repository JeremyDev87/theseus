# Theseus

**Distribution parity tests for packaged CLIs.**

Theseus answers one release question:

> Does the same CLI artifact preserve its declared behavior when the installation profile changes?

A smoke test only proves that commands ran. Theseus packs one npm artifact, installs it into isolated profiles, and compares exit code, normalized stdout/stderr, JSON expectations, and runtime identity. It distinguishes **behavior drift** from **incomplete evidence**.

## Status

Theseus is a kill-gated `v0.1.0` Go prototype for npm CLI packages on the current host. No npm package, native binary, tag, or release has been published.

Node.js and npm remain target-workload prerequisites: the Theseus runtime is native Go, but npm lifecycle scripts and target CLIs still execute.

## Local use

```bash
go build -trimpath -o build/theseus ./cmd/theseus
./build/theseus verify @jeremyfellaz/maximus@0.1.4 \
  --contract testdata/corpus/maximus.json
```

Machine-readable output:

```bash
./build/theseus verify ./candidate.tgz \
  --contract theseus.config.json \
  --format json
```

Compare two schema-v2 reports by stable evidence identity:

```bash
./build/theseus diff before.json after.json --format text
./build/theseus diff before.json after.json --format json
```

`diff` classifies evidence as `introduced`, `resolved`, or `persisted`. It exits `1` only when the after report introduces evidence, `0` when it does not, and `2` when either input is malformed or does not satisfy the schema/identity contract.

## Contract

```json
{
  "version": 1,
  "bin": "example",
  "profiles": {
    "default": { "installArgs": [] },
    "noOptional": { "installArgs": ["--omit=optional"] }
  },
  "probes": [
    {
      "id": "help",
      "argv": ["--help"],
      "timeoutMs": 10000,
      "compare": ["exit", "stdout", "stderr", "runtime"],
      "expect": { "exit": 0 }
    }
  ]
}
```

The version-1 parser is fail-closed: unknown fields, duplicate IDs/profile pairs, invalid references, unsupported normalizer syntax, and broad allowed deltas are rejected.

### Source subject

A local source command can participate as another subject:

```json
{
  "source": {
    "command": "node",
    "args": ["bin/example.js"],
    "cwd": "."
  }
}
```

Every declared field is compared across every subject/profile pair. Subject roots and CRLF line endings are normalized automatically.

Additional normalizers use a conservative ECMAScript/RE2 intersection. Flags are limited to `g`, `i`, `m`, and `s`; `g` controls global replacement. Inline mode groups, Go-only escapes/POSIX classes, lookarounds, named groups, and backreferences fail closed. Replacement expansion accepts `$$` for a literal dollar and existing single-digit captures `$1` through `$9`; JavaScript-only `$&` and Go-only `$0` are rejected rather than silently changing evidence.

### JSON output paths

`expect.stdoutJson` accepts dotted paths for simple object keys and RFC 6901 JSON Pointer for dotted keys, escaped tokens, and arrays:

```json
{
  "expect": {
    "stdoutJson": [
      { "path": "meta.version", "type": "string" },
      { "path": "/meta/a.b", "type": "number" },
      { "path": "/items/0/name", "type": "string" }
    ]
  }
}
```

JSON Pointer uses `~1` for `/` and `~0` for `~`.

### Exact allowed deltas

An intentional difference is admitted only for one `probe × profile pair × field`, with exact values or SHA-256 digests:

```json
{
  "probe": "help",
  "between": ["default", "noOptional"],
  "field": "stdout",
  "fromSha256": "<default stdout sha256>",
  "toSha256": "<no-optional stdout sha256>",
  "reason": "documented reduced fallback help"
}
```

Exit deltas use exact `from` and `to` integers. Allowing one field never suppresses drift in another field.

## Exit contract

| Exit | Meaning |
|---:|---|
| `0` | Verify: all contracts hold. Diff: no evidence was introduced |
| `1` | Verify: behavior drift. Diff: evidence was introduced |
| `2` | Input/schema/contract, pack, install, bin resolution, or probe evidence is incomplete |

Stable finding families:

- `THS-PARITY-001`: exit-code drift
- `THS-PARITY-002`: stdout/stderr drift
- `THS-RUNTIME-001`: executable/runtime identity drift
- `THS-EXPECT-001/002`: exit or JSON-path expectation failure
- `THS-INCOMPLETE-001`: pack/install/probe evidence could not be completed

## Evidence captured

Each JSON report uses `schemaVersion: 2` and `identityVersion: 1`. Every finding and incomplete-evidence entry has a deterministic `ths:v1:<sha256>` ID derived only from semantic identity fields: code, probe, field or stage, canonicalized profiles, and a semantic locator. Messages, expected/actual values, output digests, stderr, and timing are deliberately excluded so presentation or observed-value changes do not break lifecycle tracking.

Reports also bind results to artifact SHA-256/size, package identity, OS/architecture, Node/npm, profile/install arguments, selected executable, package-level optional dependencies, normalized output digests, exit/signal/timeout, findings, and exact allowed deltas. `durationMs` is recorded but is not a parity field. `diff` accepts only complete schema-v2/identity-v1 reports and rejects schema v1, unknown fields, duplicate IDs, malformed JSON, and IDs that do not match their semantic fields.

Schema-v2 parsing also verifies receipt self-integrity. Artifact SHA-256 values must be lowercase 64-hex; artifact bytes are not embedded, so this is a format check rather than artifact-byte recomputation. Runtime canonical JSON and SHA-256 are rebuilt from the receipt's semantic runtime fields, and each probe's stdout/stderr SHA-256 is recomputed from its captured text. Receipt profiles must be unique and canonical, installed runtime package identity must match the artifact, and finding/allowed-difference profile, probe, field, and digest references must resolve to the observed receipt data. Internally inconsistent reports fail closed with `diff` exit `2`.

## Corpus

Pinned contracts under `testdata/corpus/` exercise Maximus, Kratos, Legolas, ast-grep, Biome, Turbo, esbuild, and Oxlint. The corpus routes every generated JSON report through the same strict schema-v2 parser before checking the pinned status/finding ledger. Build first, then run the explicit networked ledger:

```bash
go run ./cmd/corpus
```

Expected process outcomes are Maximus strict `1`, Maximus allowed `0`, Kratos `1`, Legolas `0`, ast-grep `2` (`incomplete`, never clean), and Biome/Turbo/esbuild/Oxlint `1` (`drift`). The four public optional-native packages passed a two-run stability gate on Darwin arm64 without package-specific normalizers or product-code exceptions; this is host-scoped corpus evidence, not a cross-platform claim.

## Security and scope boundaries

Theseus executes lifecycle scripts during `npm pack`/`npm install` and executes target CLI probes. Temporary directories are cleanup boundaries, **not security sandboxes**. Run only trusted packages and do not define probes that print secrets or personal data.

Not in v0.1: security/trust scoring, claims extrapolated from one host, SARIF, release ABI inference, publication, registry mutation, or automatic judgment of whether a difference is desirable.

## Development verification

```bash
gofmt -w cmd internal
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build -trimpath -o build/theseus ./cmd/theseus
go run ./cmd/corpus
```

Language-neutral migration fixtures live under `testdata/conformance/`.

## License

MIT
