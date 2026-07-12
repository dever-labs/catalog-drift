# catalog-drift — Fixes Needed

All items have been resolved. See git history for details.

| # | Item | Resolution |
|---|---|---|
| 1 | `breaking` subcommand | Deliberately folded into `check --scan-code` — a separate subcommand was redundant |
| 2 | Rename `usage` → `deprecated` | Done — `usage` kept as a backward-compatible alias |
| 3 | Fix consumer-side deprecated scanning | Done — both component-scoped (consumesApis) and catalog-wide modes |
| 4 | Add `action.yml` | Done — Docker-based GitHub Action |
| 5 | Add release workflow | Done — `.github/workflows/release.yml` |
| 6 | Backstage client missing queries | Done — `FetchConsumedAPIs`, `FetchDeprecatedAPIs`, `FetchAPIConsumers`, `FetchAllContracts` |
| 7 | MQTT falls through silently | Done — content-based detection, routed through AsyncAPI pipeline |
| 8 | AsyncAPI diff too shallow | Done — payload schema and required-field comparison per channel |
| 9 | gRPC diff regex-only | Done — `github.com/jhump/protoreflect` parser; compares services, methods, request/response types, field numbers, types, labels. Regex fallback retained for protos with unresolvable imports. |
| 10 | `main.go` too large | Done — split into `cmd_check.go`, `cmd_deprecated.go`, `cmd_consumers.go` |

