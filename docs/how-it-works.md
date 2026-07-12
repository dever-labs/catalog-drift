# How catalog-drift works

## The single source of truth

**Backstage is the authority.** Every subcommand fetches from the catalog first. No spec snapshots, no baseline files. What is registered in Backstage is what your code is held against, and who is registered as consuming an API is who gets flagged.

---

## Subcommand flows

### `check` — Does my implementation match the registered contract?

**Who runs it:** API producers, on every push.

Fetches the registered contract from Backstage and compares it against local spec files found in `--source`. With `--scan-code` it also scans code routes directly.

```
Backstage contract
       │
       ├── vs local spec files     → drift findings (missing fields, type changes, etc.)
       │
       └── vs code routes          → code-drift findings (endpoint missing from code)
              └── vs local spec    → spec-drift warning (local spec file is stale)
```

The `[local spec out of sync]` warning is advisory — it means your code and local spec disagree, but Backstage is still the authority.

```bash
catalog-drift check \
  --backstage-url https://backstage.example.com \
  --component payment-service \
  --source ./src \
  --scan-code
```

---

### `deprecated` — Am I calling anything I shouldn't be?

**Who runs it:** API consumers, on every push and PR.

Fetches all deprecated APIs from the catalog, extracts their endpoint paths, scans your source code for HTTP calls to those paths.

- **Warning** — deprecated, within grace period
- **Error** — past `--error-after` threshold

```bash
# Scoped: only check APIs your component declares it consumes
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --component order-service \
  --source ./src \
  --error-after 90d

# Catalog-wide: check against all deprecated APIs
catalog-drift deprecated \
  --backstage-url https://backstage.example.com \
  --source ./src
```

---

### `consumers` — Who is consuming my deprecated API?

**Who runs it:** API producers, before setting a sunset date.

Fetches the deprecated APIs provided by a component, then queries Backstage for all components that have declared they consume each one. No log scraping — Backstage already knows this from `consumesApis` relations.

```bash
catalog-drift consumers \
  --backstage-url https://backstage.example.com \
  --component payment-service
```

Output: a list of components that declared they consume a now-deprecated API — the teams you need to notify before sunset.

---

## Backstage annotations

| Annotation | Description | Example |
|---|---|---|
| `catalog-drift/deprecated-since` | Date the API was marked deprecated | `2024-01-15` |
| `catalog-drift/sunset-date` | Date used for informational reporting | `2024-07-15` |
| `catalog-drift/deprecation-message` | Human-readable reason | `Use /v2/payments instead` |
| `catalog-drift/successor` | Replacement API name | `payment-api-v2` |

An API is considered deprecated if `spec.lifecycle` is `deprecated`, or if `deprecated-since` is set.

---

## Output formats

All subcommands support `--format`: `text` (default), `json`, `junit`.
