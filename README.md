# catalog-drift

A CLI tool that owns the full API contract and deprecation lifecycle, using [Backstage](https://backstage.io) as the single source of truth.

Drop it into a GitHub Actions or GitLab CI pipeline to automatically catch contract violations, breaking changes, deprecated API usage, and sunset enforcement — without maintaining any separate snapshot files.

## The core idea

**Backstage is the authority.** Your code is compared directly against what is registered in the catalog. No spec files are checked into a special location. No separate diff snapshots. If your code diverges from what Backstage says you provide, or if you call something Backstage says is deprecated, the pipeline fails.

See **[How it works](docs/how-it-works.md)** for the full flow, and **[Pipeline integration](docs/pipeline.md)** for copy-paste GitHub Actions and GitLab CI examples.

## Subcommands

| Command | Who runs it | What it checks |
|---|---|---|
| `check` | Producer, on every push | Local spec files and/or code routes vs registered Backstage contract |
| `breaking` | Producer, on every push/PR | Code routes vs registered Backstage contract — fails if a registered endpoint is gone from code |
| `deprecated` | Consumer, on every push/PR | Source code for calls to any API marked deprecated in the catalog |
| `consumers` | Producer, before sunset | Who is actively calling a deprecated endpoint (Prometheus or log file) |
| `enforce` | Producer, at sunset | Generates gateway config (nginx/Envoy/Kong) to return `410 Gone` |

## Quick start

```bash
# Producer: did my code break the registered contract?
catalog-drift breaking \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --source ./src

# Consumer: am I calling anything deprecated?
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --source ./src \
  --error-after 90d
```

## GitHub Actions

```yaml
- uses: dever-labs/catalog-drift@main
  with:
    subcommand: breaking
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    component: payment-service
    source: ./src
    token: ${{ secrets.BACKSTAGE_TOKEN }}
```

See [docs/pipeline.md](docs/pipeline.md) for full pipeline examples covering all subcommands.

## Supported contract types

| Type | Format |
|---|---|
| REST | OpenAPI 3.x |
| Event-driven | AsyncAPI 2.x / 3.x |
| RPC | gRPC / Protocol Buffers |
| Messaging | MQTT (via AsyncAPI bindings) |

## Supported gateway backends

| Gateway | Output |
|---|---|
| nginx | `location` block with `return 410` |
| Envoy | `EnvoyFilter` / `VirtualService` YAML |
| Kong | Route + plugin config |

## Development

```bash
go mod download
go build ./...
go test ./...
```

Open in VS Code → "Reopen in Container" for a fully configured environment.

