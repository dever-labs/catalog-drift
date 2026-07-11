# catalog-drift

A CLI tool that owns the full API deprecation and drift lifecycle. It fetches API contracts from a [Backstage](https://backstage.io) catalog and produces four categories of findings in a single run:

- **Contract drift** — implementation deviates from the registered contract
- **Deprecated usage** — your code calls endpoints or fields marked deprecated in any contract in the catalog
- **Runtime consumer discovery** — ingests gateway access logs or metrics to surface who is actually calling a deprecated API, including undeclared consumers not in the Backstage dependency graph
- **Sunset enforcement** — generates gateway configuration (Envoy, Kong, nginx) to return `410 Gone` for endpoints that have passed their declared sunset date

Supports **OpenAPI**, **AsyncAPI**, **gRPC (proto)**, and **MQTT**. Designed to drop into GitHub Actions or GitLab CI with a single step.

## How it works

### Contract drift (producer side)

Run on the service that owns the contract. Fails CI if the implementation diverges from the registered spec.

1. **Fetch** — connects to your Backstage catalog API and retrieves registered contracts for the specified component
2. **Scan** — walks your codebase and extracts the actual API surface (routes, schemas, event definitions, proto messages)
3. **Diff** — compares the two and surfaces violations: missing fields, changed types, undeclared endpoints, schema drift
4. **Report** — outputs results as human-readable text, JSON, or JUnit XML

### Deprecated usage (consumer side)

Run on any service that consumes external APIs. Warns or fails CI if the codebase calls anything marked deprecated.

```bash
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --source ./src \
  --format text
```

### Runtime consumer discovery

Ingests gateway access logs or a Prometheus/Datadog metrics query to find services calling deprecated endpoints — including consumers not registered in the Backstage catalog. Useful for producers preparing to sunset an API.

```bash
catalog-drift consumers \
  --backstage-url https://backstage.example.com \
  --component my-service \
  --gateway prometheus \
  --prometheus-url https://prometheus.example.com \
  --lookback 30d
```

Outputs a ranked list of active consumers with last-seen timestamp and call volume, so you can reach out to teams before enforcing a sunset.

### Sunset enforcement

Generates gateway configuration to return `410 Gone` for endpoints that have passed their declared `x-sunset` date. Supports Envoy, Kong, and nginx.

```bash
catalog-drift enforce \
  --backstage-url https://backstage.example.com \
  --component my-service \
  --gateway envoy \
  --output ./gateway/deprecated-routes.yaml
```

The generated config can be committed to the gateway configuration repository and applied via your standard deployment pipeline. catalog-drift does not apply config directly — it generates and validates it.

## Usage (drift check)

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

## Supported gateway backends (sunset enforcement)

| Gateway | Output format |
|---|---|
| Envoy | EnvoyFilter / VirtualService YAML |
| Kong | Route + plugin config |
| nginx | `location` block with `return 410` |

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
