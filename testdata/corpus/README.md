# Live corpus contracts

These files are version-pinned executable evidence, not default unit tests.

The corpus ledger requires report `schemaVersion: 2` and `identityVersion: 1`; every emitted finding or incomplete-evidence entry is assigned a stable semantic ID before JSON output.

| Contract | Expected exit | Purpose |
|---|---:|---|
| `maximus.json` | `1` | Detect help, runtime, and version-exit drift even though both profiles execute |
| `maximus-allowed.json` | `0` | Prove exact, local allowed deltas can admit intentional fallback behavior |
| `kratos.json` | `1` | Detect native/no-optional command behavior drift |
| `legolas.json` | `0` | Preserve parity when the profile does not change installed runtime behavior |
| `ast-grep.json` | `2` | Keep a no-optional install failure incomplete rather than clean |
| `biome.json` | `1` | Detect the missing platform CLI package as exit/output/runtime drift |
| `turbo.json` | `1` | Detect just-in-time binary repair output and runtime-receipt drift |
| `esbuild.json` | `1` | Detect postinstall fallback as runtime-receipt drift even when probes match |
| `oxlint.json` | `1` | Detect the missing native binding as exit/output/runtime drift |

The Maximus allowed-delta digests are pinned to `0.1.4` and the Darwin arm64 corpus receipt used to establish the prototype gate. Other platforms are expected to reject those exact runtime digests rather than silently broadening the allowance.

The public optional-native kill gate is pinned to Biome `2.5.4`, Turbo `2.10.5`, esbuild `0.28.1`, and Oxlint `1.74.0`. Two fresh Darwin arm64 runs produced identical exit/status/finding-order ledgers for all four packages. Their shipped package sources explain the branches: Biome directly resolves its platform package; Turbo installs the missing binary just in time; esbuild's postinstall installs or downloads a fallback binary; and Oxlint throws after native/WASI binding resolution fails. The gate therefore records product behavior without package-specific normalizers or Theseus product-code exceptions.

**Kill-gate verdict: GO.** Four public packages exceed the threshold of three stable classifications, and no unexplained finding noise was observed. This is corpus evidence on the current host, not a cross-platform package-quality judgment.

Run with network access:

```bash
go build -trimpath -o build/theseus ./cmd/theseus
go run ./cmd/corpus
```
