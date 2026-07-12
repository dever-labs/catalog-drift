# Pipeline integration

## GitHub Actions

### Producer pipeline

```yaml
# .github/workflows/api-contract.yml
name: API contract

on: [push, pull_request]

jobs:
  check:
    name: Contract check
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: check
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          source: ./src
          scan-code: 'true'
          token: ${{ secrets.BACKSTAGE_TOKEN }}
```

### Consumer pipeline

```yaml
# .github/workflows/deprecated-usage.yml
name: Deprecated API usage

on: [push, pull_request]

jobs:
  deprecated:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: deprecated
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: order-service   # optional: scope to declared consumesApis
          source: ./src
          error-after: 90d
          token: ${{ secrets.BACKSTAGE_TOKEN }}
```

### Pre-sunset: who declared they consume my deprecated API?

```yaml
# .github/workflows/consumers.yml
name: Consumer discovery

on:
  workflow_dispatch:
  schedule:
    - cron: '0 9 * * 1'   # weekly

jobs:
  consumers:
    runs-on: ubuntu-latest
    steps:
      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: consumers
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          token: ${{ secrets.BACKSTAGE_TOKEN }}
```

---

## GitLab CI

```yaml
stages:
  - contract

contract-check:
  stage: contract
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift check
        --backstage-url "$BACKSTAGE_URL"
        --component payment-service
        --source ./src
        --scan-code
        --format junit > contract-report.xml
  artifacts:
    reports:
      junit: contract-report.xml

deprecated-usage:
  stage: contract
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift deprecated
        --backstage-url "$BACKSTAGE_URL"
        --component order-service
        --source ./src
        --error-after 90d
```

---

## Finding kinds

| Kind | Subcommand | Meaning |
|---|---|---|
| `drift` | `check` | Local spec diverges from Backstage contract |
| `code-drift` | `check --scan-code` | Code route missing from or extra vs Backstage contract |
| `spec-drift` | `check --scan-code` | Code route and local spec disagree (advisory) |
| `deprecation` | `check` | Contract is marked deprecated in Backstage |
| `deprecated-usage` | `deprecated` | Source code calls a deprecated endpoint |
| `consumer` | `consumers` | Component declared it consumes a deprecated API |

## Flags reference

### Common

| Flag | Default | Description |
|---|---|---|
| `--backstage-url` | _(required)_ | Backstage base URL |
| `--component` | _(required)_ | Component name in the catalog |
| `--namespace` | `default` | Backstage namespace |
| `--token` | `""` | Bearer token (or `BACKSTAGE_TOKEN` env var) |
| `--format` | `text` | `text`, `json`, or `junit` |
| `--fail-on-warn` | `false` | Exit 1 on warnings too |

### `check`

| Flag | Description |
|---|---|
| `--source` | Directory to scan (default: `.`) |
| `--scan-code` | Also scan code routes vs Backstage and local spec |
| `--error-after` | Grace period before deprecated contract becomes an error |

### `deprecated`

| Flag | Description |
|---|---|
| `--source` | Directory to scan for deprecated API calls |
| `--error-after` | Grace period before deprecated usage becomes an error |
