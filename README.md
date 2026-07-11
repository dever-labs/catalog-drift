# catalog-drift

A CLI tool that fetches API contracts from a [Backstage](https://backstage.io) catalog and scans your codebase to detect drift between your registered contracts and actual implementation.

Supports **OpenAPI**, **AsyncAPI**, **gRPC (proto)**, and **MQTT**. Designed to drop into GitHub Actions or GitLab CI with a single step.

## How it works

1. **Fetch** — connects to your Backstage catalog API and retrieves registered contracts for the specified component
2. **Scan** — walks your codebase and extracts the actual API surface (routes, schemas, event definitions, proto messages)
3. **Diff** — compares the two and surfaces any violations: missing fields, changed types, undeclared endpoints, schema drift
4. **Report** — outputs results as human-readable text, JSON, or JUnit XML for CI integration

## Usage

```bash
catalog-drift \
  --backstage-url https://backstage.example.com \
  --component my-service \
  --source ./src \
  --format text
```

## GitHub Actions

```yaml
- name: Check API drift
  uses: dever-labs/catalog-drift@main
  with:
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    component: my-service
    source: ./src
```

## GitLab CI

```yaml
api-drift:
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift
        --backstage-url $BACKSTAGE_URL
        --component my-service
        --source ./src
        --format junit > drift-report.xml
  artifacts:
    reports:
      junit: drift-report.xml
```

## Supported contract types

| Type | Format | Scanner |
|---|---|---|
| REST | OpenAPI 3.x | Route + schema extraction |
| Event-driven | AsyncAPI 2.x / 3.x | Message + channel extraction |
| RPC | gRPC / Protocol Buffers | Proto message + service extraction |
| Messaging | MQTT (via AsyncAPI) | Topic + payload extraction |

## Development

Open in VS Code with the Dev Container for a fully configured Go environment:

```
code .
# → Reopen in Container
```

Or locally (requires Go 1.24+):

```bash
go mod download
go build ./...
go test ./...
```

## Status

🚧 Early development
