# catalog-drift

A CLI for API contract drift and deprecation detection, using
[Backstage](https://backstage.io) as the single source of truth. It drops into
a GitHub Actions or GitLab CI pipeline to catch contract violations and
deprecated API usage before they reach production.

Your code is compared directly against what is registered in the catalog — no
spec snapshots, no baseline files. If your code diverges from what Backstage
says you provide, or calls something Backstage says is deprecated, the
pipeline fails.

## Commands

| Command      | Who runs it             | What it does                                                                         |
| ------------ | ----------------------- | ------------------------------------------------------------------------------------ |
| `check`      | Producer, every push    | Diffs local spec files and/or code routes against the registered Backstage contract  |
| `deprecated` | Consumer, every push/PR | Scans source code for deprecated calls, undeclared API dependencies and removed APIs |
| `consumers`  | Producer, pre-sunset    | Lists catalog components that declared they consume a deprecated API                 |

Governance policies (for example "deprecated usage becomes a pipeline error
after 90 days") are configured once in Backstage with the
[Backstage plugin](https://github.com/dever-labs/catalog-drift/tree/main/plugins/catalog-drift),
and the CLI picks them up automatically.

## Guides

- [How it works](how-it-works.md) — the full flow, from catalog to verdict
- [Pipeline integration](pipeline.md) — copy-paste GitHub Actions and GitLab
  CI examples
