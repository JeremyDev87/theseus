# Live corpus contracts

These files are version-pinned executable evidence, not default unit tests.

| Contract | Expected exit | Purpose |
|---|---:|---|
| `maximus.json` | `1` | Detect help, runtime, and version-exit drift even though both profiles execute |
| `maximus-allowed.json` | `0` | Prove exact, local allowed deltas can admit intentional fallback behavior |
| `kratos.json` | `1` | Detect native/no-optional command behavior drift |
| `legolas.json` | `0` | Preserve parity when the profile does not change installed runtime behavior |
| `ast-grep.json` | `2` | Keep a no-optional install failure incomplete rather than clean |

The Maximus allowed-delta digests are pinned to `0.1.4` and the Darwin arm64 corpus receipt used to establish the prototype gate. Other platforms are expected to reject those exact runtime digests rather than silently broadening the allowance.

Run with network access:

```bash
npm run corpus:live
```
