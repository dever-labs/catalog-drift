# Pipeline integration

## GitHub Actions

### Producer pipeline — every push

Runs on every push to catch contract violations and breaking changes before they reach consumers.

```yaml
# .github/workflows/api-contract.yml
name: API contract

on:
  push:
    branches: [main]
  pull_request:

jobs:
  breaking:
    name: Breaking changes
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Check for breaking changes
        uses: dever-labs/catalog-drift@main
        with:
          subcommand: breaking
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          source: ./src                        # scans actual code vs Backstage contract
          token: ${{ secrets.BACKSTAGE_TOKEN }}

  check:
    name: Contract drift
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Check spec + code drift
        uses: dever-labs/catalog-drift@main
        with:
          subcommand: check
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          source: ./src
          scan-code: 'true'                    # also check code routes; warns if local spec is stale
          token: ${{ secrets.BACKSTAGE_TOKEN }}
          format: junit                        # optional: publish as test results

      - name: Publish results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: contract-report
          path: "*.xml"
```

### Consumer pipeline — every push

Runs on services that call external APIs. Warns during the grace period, errors after it.

```yaml
# .github/workflows/deprecated-usage.yml
name: Deprecated API usage

on: [push, pull_request]

jobs:
  deprecated:
    name: Deprecated usage
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: deprecated
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: order-service             # optional: scope to declared consumesApis
          source: ./src
          error-after: 90d                     # warning for 90 days, then error
          token: ${{ secrets.BACKSTAGE_TOKEN }}
```

### Pre-sunset: who is still calling my deprecated endpoint?

Run this as a scheduled job or manually before setting a sunset date. Tells you which services to notify.

```yaml
# .github/workflows/consumers.yml
name: Consumer discovery

on:
  workflow_dispatch:                           # manual trigger
  schedule:
    - cron: '0 9 * * 1'                       # weekly on Monday

jobs:
  consumers:
    runs-on: ubuntu-latest
    steps:
      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: consumers
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          gateway: prometheus
          token: ${{ secrets.BACKSTAGE_TOKEN }}
        env:
          PROMETHEUS_URL: ${{ secrets.PROMETHEUS_URL }}
```

### Sunset enforcement

Generates gateway config for endpoints past their sunset date. Commit the output and deploy.

```yaml
# .github/workflows/enforce-sunset.yml
name: Enforce sunset

on:
  workflow_dispatch:
  schedule:
    - cron: '0 6 * * *'                       # daily

jobs:
  enforce:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: dever-labs/catalog-drift@main
        with:
          subcommand: enforce
          backstage-url: ${{ secrets.BACKSTAGE_URL }}
          component: payment-service
          gateway: nginx
          output: ./gateway/sunset-routes.conf
          token: ${{ secrets.BACKSTAGE_TOKEN }}

      - name: Commit gateway config
        run: |
          git config user.name  "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add ./gateway/sunset-routes.conf
          git diff --cached --quiet || git commit -m "chore: update sunset gateway rules"
          git push
```

---

## GitLab CI

### Producer — every push

```yaml
# .gitlab-ci.yml

stages:
  - contract

breaking-changes:
  stage: contract
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift breaking
        --backstage-url "$BACKSTAGE_URL"
        --component payment-service
        --source ./src
  only: [merge_requests, main]

contract-drift:
  stage: contract
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift check
        --backstage-url "$BACKSTAGE_URL"
        --component payment-service
        --source ./src
        --scan-code
        --format junit
        > contract-report.xml
  artifacts:
    reports:
      junit: contract-report.xml
  only: [merge_requests, main]
```

### Consumer — every push

```yaml
deprecated-usage:
  stage: contract
  image: ghcr.io/dever-labs/catalog-drift:latest
  script:
    - catalog-drift deprecated
        --backstage-url "$BACKSTAGE_URL"
        --component order-service
        --source ./src
        --error-after 90d
  only: [merge_requests, main]
```

---

## Understanding the output

### Text output

```
catalog-drift — payment-service
────────────────────────────────────────────────────────────
API: payment-api

  ✗ [error]   path "/payments/{id}" is declared in the contract but no route
              was found in the code (paths./payments/{id})

  ⚠ [warning] [local spec out of sync] path "/payments/bulk" exists in the
              code but is not declared in the local spec file

────────────────────────────────────────────────────────────
1 error(s), 1 warning(s)
```

- `✗ [error]` — pipeline fails (exit code 1)
- `⚠ [warning]` — advisory; add `--fail-on-warn` to also fail on warnings

### Finding kinds

| Kind | Subcommand | Meaning |
|---|---|---|
| `breaking` | `breaking` | Registered endpoint missing from code |
| `drift` | `check` | Local spec diverges from Backstage contract |
| `code-drift` | `check --scan-code` | Code route missing from Backstage contract |
| `spec-drift` | `check --scan-code` | Code route missing from local spec file (advisory) |
| `deprecation` | `check` | Contract is marked deprecated in Backstage |
| `deprecated-usage` | `deprecated` | Source code calls a deprecated endpoint |
| `consumer` | `consumers` | Service actively calling a deprecated endpoint |

---

## Flags reference

### Common flags (all subcommands)

| Flag | Default | Description |
|---|---|---|
| `--backstage-url` | _(required)_ | Backstage base URL |
| `--component` | _(required)_ | Component name in the catalog |
| `--namespace` | `default` | Backstage namespace |
| `--token` | `""` | Bearer token (or set `BACKSTAGE_TOKEN` env var) |
| `--format` | `text` | Output format: `text`, `json`, `junit` |
| `--fail-on-warn` | `false` | Also exit 1 on warnings |

### `breaking`

| Flag | Description |
|---|---|
| `--source` | Directory to scan for code routes (default: `.`) |

### `check`

| Flag | Description |
|---|---|
| `--source` | Directory to scan for spec files and code routes (default: `.`) |
| `--scan-code` | Also diff code routes against Backstage contract and local spec |
| `--error-after` | Duration after `deprecated-since` before emitting an error (e.g. `90d`) |

### `deprecated`

| Flag | Description |
|---|---|
| `--source` | Directory to scan for deprecated API calls (default: `.`) |
| `--error-after` | Grace period before treating deprecated usage as an error |

### `consumers`

| Flag | Default | Description |
|---|---|---|
| `--gateway` | `prometheus` | Source: `prometheus` or `file` |
| `--prometheus-url` | | Prometheus base URL |
| `--prometheus-metric` | `http_requests_total` | Counter metric name |
| `--lookback` | `30d` | Query window (e.g. `30d`, `7d`) |
| `--logs-file` | | Path to access log file (for `--gateway file`) |
| `--logs-format` | `nginx` | Log format: `nginx`, `envoy`, `kong`, `prometheus` |

### `enforce`

| Flag | Default | Description |
|---|---|---|
| `--gateway` | `nginx` | Output format: `nginx`, `envoy`, `kong` |
| `--output` | `-` | Output file path (`-` for stdout) |
