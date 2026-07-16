# Theseus

**Distribution parity tests for packaged CLIs.**

Theseus answers one release question:

> Does the same CLI artifact preserve its declared behavior when the installation profile changes?

A smoke test can prove that two commands ran. Theseus compares the resulting exit code, normalized stdout/stderr, JSON expectations, and runtime identity, then distinguishes **behavior drift** from **incomplete evidence**.

## Status

Theseus is a kill-gated `v0.1.0` prototype. It currently supports npm CLI packages on the current host. It is not published to npm yet.

## Local use

```bash
npm ci
npm run build
node bin/theseus.js verify @jeremyfellaz/maximus@0.1.4 \
  --contract test/corpus/maximus.json
```

Machine-readable output:

```bash
node bin/theseus.js verify ./candidate.tgz \
  --contract theseus.config.json \
  --format json
```

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

The contract parser is fail-closed: unknown fields, duplicate probe IDs, invalid profile references, and broad allowed deltas are rejected.

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

Every declared field is compared across every subject/profile pair. Temporary subject roots and CRLF line endings are normalized automatically; additional text normalization must be declared explicitly.

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
| `0` | All declared contracts hold; exact allowed deltas may be present |
| `1` | Behavior drift or expectation failure |
| `2` | Contract, pack, install, bin resolution, or probe evidence is incomplete |

Stable finding families start with `THS-`:

- `THS-PARITY-001`: exit-code drift
- `THS-PARITY-002`: stdout/stderr drift
- `THS-RUNTIME-001`: executable/runtime identity drift
- `THS-EXPECT-001/002`: declared exit or JSON-path expectation failure
- `THS-INCOMPLETE-001`: pack/install/probe evidence could not be completed

## Evidence captured

Each JSON report binds results to:

- package name/version, tarball filename, size, and SHA-256;
- OS, architecture, Node, npm, profile, and install arguments;
- selected bin and resolved executable path;
- present package-level optional dependencies;
- normalized stdout/stderr plus SHA-256;
- exit code, signal, timeout, findings, and exact allowed deltas.

## Corpus

Pinned contracts under `test/corpus/` exercise:

- Maximus: both profiles run, but help/runtime/version behavior drifts;
- Kratos: native and no-optional behavior drifts;
- Legolas: both installation profiles preserve the tested contract;
- ast-grep: no-optional installation fails and must remain `incomplete`, never `clean`.

Run the networked corpus explicitly:

```bash
npm run corpus:live
```

## Security and scope boundaries

Theseus executes package lifecycle scripts during `npm pack`/`npm install` and executes the selected CLI probes. Temporary directories are cleanup boundaries, **not security sandboxes**. Run it only on packages you trust to execute. Reports contain captured stdout/stderr, so do not define probes that print secrets or personal data.

Not in v0.1:

- CVE, malware, provenance, or trust scoring;
- cross-platform claims from one host;
- SARIF, release ABI inference, publication, or registry mutation;
- automatic interpretation of whether a behavioral difference is desirable.

## Development verification

```bash
npm run verify
npm pack --dry-run --json
npm run corpus:live
```

## License

MIT
