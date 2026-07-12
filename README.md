# catalog-drift

A CLI tool for API contract drift and deprecation detection, using [Backstage](https://backstage.io) as the single source of truth.

Designed to drop into a GitHub Actions or GitLab CI pipeline to catch contract violations and deprecated API usage before they reach production.

## The core idea

**Backstage is the authority.** Your code is compared directly against what is registered in the catalog — no spec snapshots, no baseline files. If your code diverges from what Backstage says you provide, or if you call something Backstage says is deprecated, the pipeline fails.

See **[How it works](docs/how-it-works.md)** for the full flow and **[Pipeline integration](docs/pipeline.md)** for copy-paste GitHub Actions and GitLab CI examples.

## Commands

| Command | Who runs it | What it does |
|---|---|---|
| `check` | Producer, every push | Diffs local spec files and/or code routes against the registered Backstage contract |
| `deprecated` | Consumer, every push/PR | Scans source code for deprecated calls, undeclared API dependencies, and removed APIs |
| `consumers` | Producer, pre-sunset | Lists catalog components that declared they consume a deprecated API |

## Quick start

```bash
# Producer: does my code match the registered contract?
catalog-drift check \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --source ./src \
  --scan-code

# Consumer: am I calling anything deprecated?
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --source ./src \
  --error-after 90d
```

## GitHub Actions

```yaml
# Producer — check contract on every push
- uses: dever-labs/catalog-drift@main
  with:
    subcommand: check
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    component: payment-service
    source: ./src
    scan-code: 'true'
    token: ${{ secrets.BACKSTAGE_TOKEN }}

# Consumer — fail on deprecated API usage
- uses: dever-labs/catalog-drift@main
  with:
    subcommand: deprecated
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    source: ./src
    error-after: 90d
    token: ${{ secrets.BACKSTAGE_TOKEN }}
```

See [docs/pipeline.md](docs/pipeline.md) for full examples covering all commands.

## Supported contract types

| Type | Format |
|---|---|
| REST | OpenAPI 3.x |
| Event-driven | AsyncAPI 2.x / 3.x |
| RPC | gRPC / Protocol Buffers |
| Messaging | MQTT (via AsyncAPI bindings) |

## Development

```bash
go mod download
go build ./...
go test ./...
```

Open in VS Code → "Reopen in Container" for a fully configured Go environment.
