# Reuse and Dependencies

Read when adding a dependency or deciding whether to reuse, adapt, or build substantial functionality. Keep the research proportional to the decision and within the scope in [Coding Practices](../SKILL.md).

## Establish the requirement

Name the operation, required behavior, execution environment, and installed language/framework versions. Identify the integration point and relevant failure semantics. These constraints decide whether an existing solution fits; a package name or a similar README example does not establish compatibility.

Look for the behavior in the project, then its standard library, framework, and installed packages. For a substantial remaining gap, consult current primary documentation and plausible maintained packages. Use available relevant search channels without requiring a particular CLI, MCP server, sibling skill, or researcher agent.

## Choose by total ownership

| Choice | Use when | Check before proceeding |
| --- | --- | --- |
| Reuse existing behavior | The current implementation meets the same contract | Its callers, ownership, and error semantics fit the new path. |
| Adopt a package directly | It handles a meaningful requirement with acceptable integration cost | The actual version supports required APIs, runtime/platform, license needs, and dependency constraints. |
| Adapt or wrap | The dependency fits but the application needs policy or translation | Name what the adapter owns; avoid forwarding every upstream method through a second API. |
| Build locally | The logic is small and stable, or suitable existing options do not meet the requirement | Keep the implementation limited to the demonstrated gap and verify its behavior. |

Consider maintenance, updates, transitive dependencies, operational requirements, and reversibility alongside functionality. Popularity and an invented score do not establish suitability. Stop researching when the evidence supports a choice proportional to its cost; tiny local transformations do not need a package survey.

## Integrate and verify

Check the exact API against installed source or documentation for the chosen version. Follow existing package-manager and lockfile practices when a dependency change is authorized. Exercise the capability through the real integration point, including a relevant failure case. Keep application reporting, cancellation, and resource ownership intact.

For example, a CSV writer may already handle quoting and escaping, while preserving tracking identifiers as strings remains the application's responsibility. Verify both concerns with literal expected output. A wrapper is useful only if it owns a shared export contract beyond forwarding the writer call.

If a source or tool is unavailable, state what was and was not checked. Use verified local capabilities where sufficient; do not claim a dependency supports an unverified API. A materially unresolved compatibility question remains a limitation to resolve before relying on that capability.

Provenance and deliberate adaptations: [Sources](../SOURCES.md).
