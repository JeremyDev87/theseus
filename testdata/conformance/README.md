# Go migration conformance fixtures

These language-neutral fixtures freeze contract-v1 parsing and comparison behavior independently of process duration. The executable oracle is the TypeScript v0.1 tree at repository SHA `e5de7fad62613e59bc3f5733ba560f01e6d57e40`. `durationMs` is evidence but is intentionally the only excluded parity field. Live TS→Go migration evidence additionally requires the pinned corpus to produce identical status, exit code, finding codes/order, receipts, artifact identity, and report fields after removing only `durationMs`.

Normalizer patterns use the documented conservative ECMAScript/RE2 intersection. Flags are limited to `g`, `i`, `m`, and `s`; unsupported syntax or flags fail closed as incomplete contract evidence. Replacements accept only `$$` and existing single-digit captures `$1` through `$9`; incompatible `$&`, `$0`, named, or missing captures are rejected.
