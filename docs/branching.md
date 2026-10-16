# Branching workflow

[Contributor documentation](README.md) · [Contributor entry](../AGENTS.md) · [Contributing](../CONTRIBUTING.md)

Effective 2026-10-16. Core uses `main` as its integration branch and one active `dev/vX.Y.Z` development batch by default. `main` and `master` have identical Git semantics; the repository configuration selects the default branch and protection rules. Core keeps its existing `main` name.

New working branches use `dev/*` for version development, `feat/*` for independent feature work and `hotfix/*` for urgent production fixes. This project naming convention applies to every development tool and session.

## Required session preflight

Every session reads this document before task work, including documentation changes and reviews. Before editing, run:

```sh
git branch --show-current
git status --short --branch
git branch -vv
git worktree list
```

Identify the target batch, existing changes and their ownership, upstream divergence, detached HEAD or unfinished Git operations, and shared checkout usage. Worktree listings show checkout locations; coordinate with known active sessions to establish write ownership. Refresh relevant remote refs before creating a branch, integrating or pushing. Report unavailable remote verification and defer publication until it succeeds.

State the current branch, worktree state and intended action before writing. Reuse the applicable development branch. When the target batch is established and the checkout is clean, create a missing `dev/vX.Y.Z` from the verified integration baseline as part of the authorized development task. Version examples describe the naming convention; select the actual batch from the task and release records.

When the checkout belongs to another batch, changes have unclear ownership, or Git has an unfinished operation, explain the mismatch and recommend a concrete base and branch. Resolve missing decisions or shared ownership before dependent edits. Preserve existing work. Read-only reviews keep the checkout unchanged. Small standalone documentation, governance or acceptance-record changes may be edited directly on a clean `main`; behavior and contract changes travel with their development branch.

## Development and integration

The default flow is `main → dev/vX.Y.Z → main`. Work directly on the active dev branch for serial development; keep small fixes and documentation adjustments on the current task branch. For parallel implementation or isolated validation, use `dev/vX.Y.Z → feat/<topic> → dev/vX.Y.Z`. Integrate accepted features into dev, then deliver the completed batch to main.

- Keep implementation, observable-behavior tests and relevant documentation together. Group commits by complete feature or fix.
- For each feat, record the target dev branch, starting SHA, worktree path and owning session. Create it from the target dev in a separate worktree after checking existing checkouts. The owner commits any required pending changes before other tasks branch from that baseline.
- Each parallel implementation session works in its assigned branch and directory. Reuse suitable worktrees and use one checkout per branch. One session owns dev integration, conflict resolution, integration verification and version closeout.
- Worktrees isolate files and pending edits; branches retain commits. Coordinate shared test ports, external services and other shared resources separately. Keep dev history stable while feature branches depend on it; coordinate dependent sessions before rewriting it.
- Complete business and design reviews and the AGENTS quality gate before integration. Follow CONTRIBUTING for applicable checks. Verify affected behavior after each integration and combined behavior once related features are integrated.
- Synchronize feat with its target dev, and dev with main. Merge updates when others depend on a branch. An exclusively owned feat whose history can be rewritten may consolidate temporary commits and rebase onto the latest target dev, then verify and integrate into dev through fast-forward or the repository's PR workflow.
- Before dev closeout, integrate every feature planned for the release and record deferred work and its next target. Complete version-level review and integration checks. A dev branch whose rewrite is coordinated with all dependents may consolidate commits, rebase onto main, verify and fast-forward merge. Shared branches may merge their target, verify and fast-forward when ancestry permits. Concurrent target updates require renewed synchronization and verification.
- Follow actual pull-request and branch-protection requirements. Preserve meaningful feature boundaries during integration. Mainline history rewriting requires separate authorization.
- After accepted changes are retained in the target branch and remotely, and owning sessions have finished, inspect pending edits and useful ignored files before cleaning up worktrees and completed branches. Use the managing tool's archive or cleanup mechanism for tool-managed worktrees. Close abandoned proposals after retaining useful findings.

## Urgent fixes and releases

Fix development-version issues on the development branch. For an urgent production fix, create `hotfix/<topic>` from the actual deployed release tag or verified source SHA. Review and test the fix, and build its release from that branch so unrelated integration work stays outside the patch release.

Integrate the fix into `main`, then synchronize the active development branch using merge or cherry-pick as appropriate. Check for an existing equivalent fix before applying it. For an old-version-only defect, verify and record the correct integration-branch behavior. Close the hotfix after release verification and synchronization are complete.

Create immutable `vX.Y.Z` tags after release acceptance, following CONTRIBUTING. Pushing a version tag can publish artifacts; follow the release authorization for that operation. A development batch name describes planned work, while release tags and records identify shipped source. Keep existing tags stable.

When Core is consumed as a submodule, push the referenced Core commit before publishing the parent pointer. Development integration may reference a pushed development commit; the parent integration branch should reference completed Core work already integrated into `main`. Recheck pointers after rewriting Core development history. Keep this repository independently readable and include only authorized Core content.

## Completion evidence

Report the branch, scope, business review, design review, checks actually run and remaining risks. Distinguish code readiness, integration and release status. These instructions define session behavior; repository protection and CI provide enforced Git controls.
