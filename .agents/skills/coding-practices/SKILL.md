---
name: coding-practices
description: Implement features, fix bugs, and refactor code with clear ownership, useful boundaries, proportionate tests, and existing project conventions. Use when deciding names, file placement, module interfaces, dependencies, or error handling, including code that is difficult for humans or agents to navigate.
version: 1.0.0
source: local
license: MIT
---

# Coding Practices

Make the next correct change easy to locate, understand, and verify. Optimize for the amount of knowledge a maintainer must hold, not line count or the number of abstractions.

## Work from the actual project

1. Read applicable instructions, the manifest, relevant tests, and a nearby implementation. Inspect the working diff and preserve unrelated edits. Trace the affected entry points to the behavior owner and its side effects.
2. Identify the requested observable result, existing contracts, and a check that would detect an incorrect implementation. Keep possible future requirements separate from this task.
3. Follow the project's naming, framework conventions, and dependency direction. Put the behavior in its existing home. Reorganize only when that directly resolves the requested problem.
4. Implement a complete path, including failure behavior. Remove leftovers created by your change. Run focused verification and required project gates; inspect the final diff.

Scale this loop to the change. Routine edits need neither an architecture document nor a new planning system. A review or plan request remains read-only; ordinary implementation does not authorize an unrelated project cleanup.

## Read by need

| When the work involves | Read |
| --- | --- |
| Introducing or changing module boundaries, moving responsibilities, or deciding whether to extract or consolidate code | [Module design](references/module-design.md) |
| Adding a dependency or choosing whether to reuse, adapt, or build substantial functionality | [Reuse and dependencies](references/reuse-and-dependencies.md) |
| Maintaining this skill or inspecting attribution and adaptation choices | [Sources](SOURCES.md) |

Keep routine naming, local bug fixes, and mechanical edits within the core workflow unless a reference's decision is involved.

## Choose structure by the knowledge it contains

Use the project's domain vocabulary and searchable, consistent names. Keep things that must be understood and changed together close, within framework conventions. Split independent responsibilities, not arbitrary line limits.

Give a function or module a clear responsibility and a useful interface that hides implementation knowledge. Count caller obligations, ordering, errors, and configuration as part of interface complexity. Add a wrapper or layer only for a current policy, translation, lifecycle, external boundary, or independent change it isolates. A hypothetical second implementation is insufficient; one implementation does not invalidate a real boundary.

Share a domain decision that must change consistently. Keep superficially identical code separate when its rules can evolve independently. Introduce options, registries, retries, fallbacks, or compatibility paths only for a concrete requirement or established contract.

Use ordinary control flow, precise types, and explicit state transitions. Separate calculation from I/O when it makes behavior easier to understand or exercise. Comment on constraints and non-obvious reasons; let names and code explain mechanics.

## Reuse before taking ownership

Search project code using the repository's preferred search or symbol tools, then check the standard library, framework, and installed packages. For substantial commodity functionality, check current official documentation and suitable packages before implementing it yourself.

Adopt a package when it removes more maintenance than integration, updates, licensing, platform compatibility, and transitive dependencies add. Verify the needed API against the project's version. Keep small, stable domain logic local. Wrap a dependency only when the wrapper owns useful policy or translation. If research is unavailable, identify what remains unverified instead of inventing package capabilities.

## Preserve meaning at boundaries

- Validate and normalize untrusted data where it enters. Check every independent entry path: HTTP, jobs, CLI, plugins, persisted data. Internal code may rely on an invariant only after all its callers establish it.
- Apply documented defaults in one authoritative place. Distinguish missing values from valid `0`, `false`, or empty values. Reject malformed input according to the contract; avoid scattered coercions, unchecked casts, and silent repairs.
- Catch errors where recovery, cleanup, translation, or reporting is possible. Preserve the original cause and add useful operation and resource context. Keep failure distinguishable from successful empty results.
- Use the existing logger at the boundary responsible for reporting. Propagate context instead of logging the same failure at each layer. Keep operational diagnostics useful without exposing secrets.
- Preserve authorization, resource cleanup, cancellation, and transaction guarantees. Simpler code must still honor these responsibilities.

## Verify behavior, proportionately

Add or adapt a test when the change introduces a meaningful behavior, fixes a reproducible bug, or exposes a concrete protection gap. For a bug, reproduce the failure before fixing it when feasible. Reuse existing checks for mechanical edits.

State the incorrect result each test would catch. Derive expectations from requirements, known examples, or a documented contract, independently of the production transformation. Use focused logic tests for meaningful rules and integration tests for wiring or cross-component behavior. Exercise important failure paths; avoid testing private structure or repeating mock setup as proof.

Run the relevant existing checks and required gates. Never obtain green results by weakening assertions, disabling checks, or changing discovery. Report pre-existing failures and unavailable verification accurately.

## Finish

Report what changed, why it is easier to maintain, checks actually run, and remaining limitations. Reconsider any added file, dependency, or abstraction whose present responsibility you cannot explain. Stop when the requested behavior and required verification are complete.
