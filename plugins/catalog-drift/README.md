# @dever-labs/backstage-plugin-catalog-drift

Backstage plugin for **catalog-drift** API governance. Provides administrators
with a portal UI to configure deprecation policies that the `catalog-drift` CLI
automatically picks up in pipelines — no per-repo flag configuration needed.

---

## What it does

| Feature | Description |
|---|---|
| **GovernancePolicyPage** | Admin page: list, create, and edit `GovernancePolicy` entities |
| **ApiGovernanceCard** | Card on the API entity page showing deprecation status and active policy |

### How policies flow

```
Admin creates GovernancePolicy in Backstage
            ↓
Stored as a catalog entity (kind: GovernancePolicy)
            ↓
catalog-drift CLI fetches the policy at runtime
            ↓
Pipeline gets consistent governance — no per-repo flag needed
```

Policy resolution order in the CLI:
1. Policy named by the component's `catalog-drift/governance-policy` annotation.
2. Policy named `default` in the component's namespace.
3. Policy named `default` in the `default` namespace (global fallback).
4. CLI flags (always override Backstage values when explicitly set).

---

## Installation

### 1. Add the plugin to your Backstage app

```bash
# From your Backstage root
yarn add --cwd packages/app @dever-labs/backstage-plugin-catalog-drift
```

### 2. Register the route (packages/app/src/App.tsx)

```tsx
import { GovernancePolicyPage } from '@dever-labs/backstage-plugin-catalog-drift';

// Inside <FlatRoutes>:
<Route path="/catalog-drift" element={<GovernancePolicyPage />} />
```

### 3. Add to sidebar (packages/app/src/components/Root/Root.tsx)

```tsx
import SecurityIcon from '@material-ui/icons/Security';

<SidebarItem icon={SecurityIcon} to="catalog-drift" text="API Governance" />
```

### 4. Add the card to the API entity page (packages/app/src/components/catalog/EntityPage.tsx)

```tsx
import { ApiGovernanceCard } from '@dever-labs/backstage-plugin-catalog-drift';

// Inside the API entity page layout:
<Grid item md={6}>
  <ApiGovernanceCard />
</Grid>
```

### 5. Register the GovernancePolicy entity kind

Add a custom entity processor so Backstage recognises the `GovernancePolicy`
kind. In `packages/backend/src/plugins/catalog.ts`:

```typescript
import { GovernancePolicyProcessor } from '@dever-labs/backstage-plugin-catalog-drift-backend';

builder.addProcessor(new GovernancePolicyProcessor());
```

> **Note:** The backend plugin (`catalog-drift-backend`) is in development.
> In the meantime, you can add policies directly via the catalog API (the
> frontend plugin's form does this automatically).

---

## GovernancePolicy entity format

```yaml
apiVersion: catalog-drift.io/v1alpha1
kind: GovernancePolicy
metadata:
  name: default          # "default" = applies to all components in this namespace
  namespace: default     # "default" namespace = global fallback for all teams
  title: Platform default policy
spec:
  deprecation:
    errorAfter: "90d"        # grace period from deprecated-since → error in CI
    warnBeforeSunset: "30d"  # start warning 30 days before sunset date
  contract:
    failOnWarn: false        # true = warnings also fail the pipeline
```

A team can have its own stricter policy:

```yaml
apiVersion: catalog-drift.io/v1alpha1
kind: GovernancePolicy
metadata:
  name: payments-strict
  namespace: payments
spec:
  deprecation:
    errorAfter: "30d"
  contract:
    failOnWarn: true
```

And opt a component into it:

```yaml
apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: payment-service
  namespace: payments
  annotations:
    catalog-drift/governance-policy: payments-strict
```

---

## Deprecation annotations on API entities

These annotations are set on `API` entities (not policies) to carry metadata
that the CLI uses for severity escalation:

| Annotation | Format | Description |
|---|---|---|
| `catalog-drift/deprecated-since` | `2024-01-15` | When the API was deprecated (ISO 8601) |
| `catalog-drift/sunset-date` | `2025-01-15` | When the API will be removed |
| `catalog-drift/deprecation-message` | string | Human-readable deprecation notice |
| `catalog-drift/successor` | API name | Name of the replacement API |
| `catalog-drift/governance-policy` | policy name | Override the policy for a component |

---

## CLI integration

Once policies are configured in Backstage, no `--error-after` flag is needed
in pipelines — the CLI reads the policy automatically:

```yaml
# .github/workflows/api-governance.yml
- uses: dever-labs/catalog-drift@main
  with:
    subcommand: deprecated
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    component: payment-service
    token: ${{ secrets.BACKSTAGE_TOKEN }}
    # No --error-after needed — policy comes from Backstage
```

CLI flags override Backstage policies when needed (e.g. testing a stricter
threshold in a specific PR):

```yaml
- uses: dever-labs/catalog-drift@main
  with:
    subcommand: deprecated
    backstage-url: ${{ secrets.BACKSTAGE_URL }}
    component: payment-service
    token: ${{ secrets.BACKSTAGE_TOKEN }}
    error-after: 30d   # overrides the Backstage policy for this run
```
