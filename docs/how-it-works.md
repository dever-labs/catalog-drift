# How catalog-drift works

## The single source of truth

**Backstage is the authority.** Every subcommand fetches data from Backstage first and works outward from there. No spec snapshots. No local baseline files. What is registered in your catalog is what your code is held against.

---

## The lifecycle

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Backstage Catalog                            │
│   component: payment-service                                        │
│     providesApis: [payment-api]                                     │
│     consumesApis: [order-api, inventory-api]                        │
│                                                                     │
│   api: payment-api  (lifecycle: production)                         │
│     spec.definition: <OpenAPI 3.x spec>                             │
│                                                                     │
│   api: order-api    (lifecycle: deprecated)                         │
│     annotations:                                                    │
│       catalog-drift/deprecated-since: 2024-01-15                   │
│       catalog-drift/sunset-date: 2024-07-15                        │
└───────────────────────────┬─────────────────────────────────────────┘
                            │  fetch via REST API
            ┌───────────────┼───────────────────────┐
            ▼               ▼                       ▼
      [breaking]        [check]              [deprecated]
   code vs catalog   spec+code vs         consumer code vs
    (producer CI)      catalog           deprecated catalog
                    (producer CI)          (consumer CI)
```

---

## Subcommand flows

### `breaking` — Did I remove something consumers depend on?

**Run by:** API producers, on every push and PR.  
**Source of truth:** Backstage catalog.  
**What it does:** Fetches the registered spec for your component, scans your actual code for implemented routes, and fails if any registered endpoint is missing from the code.

```
Backstage: GET /payments, POST /payments, DELETE /payments/{id}
Your code: GET /payments, POST /payments
                                         ↑ DELETE is gone → ERROR
```

Why code vs Backstage directly, not code vs local spec file?
Because you might update your code and forget to update your spec. Your spec file doesn't matter — what consumers depend on is what's registered in Backstage.

```bash
catalog-drift breaking \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --source ./src
```

---

### `check` — Does my implementation match the registered contract?

**Run by:** API producers, on every push.  
**Source of truth:** Backstage catalog.  
**What it does:** Compares local spec files (if present) and optionally code routes against the registered Backstage contract. Also checks for deprecation annotations.

```
Backstage contract ──────────────────┐
                                     ▼
Local spec files  ──────────────► diff → findings
                                     ▲
Code routes (--scan-code) ───────────┘
```

With `--scan-code`, two additional comparisons run:

1. **Code vs Backstage** — reports endpoints missing from code that Backstage declares (error)
2. **Code vs local spec** — warns if your local spec file is out of sync with your code (`[local spec out of sync]`)

The second check is advisory: Backstage is the authority, but it nudges you to keep your spec file up to date.

```bash
catalog-drift check \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --source ./src \
  --scan-code
```

---

### `deprecated` — Am I calling anything I shouldn't be?

**Run by:** API consumers, on every push and PR.  
**Source of truth:** Backstage catalog (lifecycle=deprecated + deprecation annotations).  
**What it does:** Fetches all deprecated APIs from the catalog, extracts their endpoint paths, then scans your source code for HTTP calls to those paths.

Severity escalation:
- **Warning** — the API is deprecated, grace period still active
- **Error** — the `--error-after` threshold has passed (default: immediately an error unless configured)

```bash
# Scope to APIs your component declares it consumes
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --component order-service \
  --source ./src \
  --error-after 90d

# Catalog-wide: check against all deprecated APIs (no --component)
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --source ./src
```

---

### `consumers` — Who is calling my deprecated endpoint right now?

**Run by:** API producers, before setting a sunset date or after marking an API deprecated.  
**Source of truth:** Prometheus metrics or gateway access logs.  
**What it does:** Queries live traffic data to find which services are actively calling a deprecated endpoint — including consumers not registered in the Backstage catalog.

```bash
# Via Prometheus
catalog-drift consumers \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --gateway prometheus \
  --prometheus-url https://prometheus.example.com \
  --lookback 30d

# Via log file
catalog-drift consumers \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --gateway file \
  --logs-file /var/log/nginx/access.log \
  --logs-format nginx
```

Output shows each consumer service, the endpoint it called, request count, and last-seen timestamp. Use this to notify teams before enforcing a sunset.

---

### `enforce` — Return 410 Gone for past-sunset endpoints

**Run by:** API producers, at or after the declared sunset date.  
**Source of truth:** Backstage catalog (`catalog-drift/sunset-date` annotation).  
**What it does:** Generates gateway configuration for any endpoint whose sunset date has passed. The generated config causes the gateway to return `410 Gone` without the request reaching your service.

```bash
catalog-drift enforce \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --gateway nginx \
  --output ./gateway/sunset-routes.conf
```

The tool generates config only — it does not apply it. Commit the output to your gateway configuration repository and deploy via your normal pipeline.

---

## Backstage annotations

catalog-drift reads the following annotations from API entities:

| Annotation | Description | Example |
|---|---|---|
| `catalog-drift/deprecated-since` | Date the API was marked deprecated | `2024-01-15` |
| `catalog-drift/sunset-date` | Date after which 410 should be enforced | `2024-07-15` |
| `catalog-drift/deprecation-message` | Human-readable reason | `Use /v2/payments instead` |
| `catalog-drift/successor` | Replacement API name | `payment-api-v2` |

An API is considered deprecated if `spec.lifecycle` is `deprecated`, or if the `deprecated-since` annotation is present.

---

## Output formats

All subcommands support `--format`:

- `text` (default) — human-readable terminal output
- `json` — structured JSON for downstream processing
- `junit` — JUnit XML for CI test result reporting
