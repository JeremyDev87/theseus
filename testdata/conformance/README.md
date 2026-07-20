# Go migration conformance fixtures

These language-neutral fixtures freeze contract-v1 parsing, comparison behavior, report schema v2, identity version 1, and report-diff lifecycle behavior independently of process duration. The executable oracle for the original comparison contract is the TypeScript v0.1 tree at repository SHA `e5de7fad62613e59bc3f5733ba560f01e6d57e40`. `durationMs` is evidence but is intentionally excluded from parity identity.

`expected/basic.json` and `expected/allowed.json` contain reviewed literal finding IDs. `reports/diff-before.json`, `reports/diff-after.json`, and `expected/diff-lifecycle.json` freeze one introduced/resolved/persisted transition without generating the expected IDs from production code. Diff inputs fail closed unless they are complete schema-v2/identity-v1 Theseus reports with unique, recomputable evidence IDs.

Live corpus evidence additionally requires pinned targets to produce the expected status, exit code, finding codes/order, identity version, receipts, artifact identity, and report fields. Report IDs intentionally exclude message wording, expected/actual values, digests, stderr, and timing.

Normalizer patterns use the documented conservative ECMAScript/RE2 intersection. Flags are limited to `g`, `i`, `m`, and `s`; unsupported syntax or flags fail closed as incomplete contract evidence. Replacements accept only `$$` and existing single-digit captures `$1` through `$9`; incompatible `$&`, `$0`, named, or missing captures are rejected.
