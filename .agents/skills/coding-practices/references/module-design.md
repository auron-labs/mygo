# Module Design

Read when changing module boundaries, moving responsibilities, or deciding whether to extract or consolidate code. Follow the actual project's architecture and the scope in [Coding Practices](../SKILL.md).

## Find the owner and the caller's burden

Trace one requested operation from entry point to result. Identify who owns each decision and which files must change together. Start with a nearby implementation and existing architectural decisions. Place new behavior in its cohesive home; a separate file is useful when it isolates a responsibility that can be understood independently.

An interface includes more than a signature: callers may need to know ordering, configuration, error cases, state, or resource ownership. Evaluate a proposed extraction by what callers can stop knowing. A short forwarding function can add another place to inspect without hiding any responsibility. A provider adapter can hide substantial translation and failure behavior even when its public surface is small.

Imagine removing the abstraction. If callers would need to repeat its policy or understand provider details, it is doing useful work. If the operation and caller obligations are unchanged, investigate whether the forwarding step is needed. One implementation alone does not decide the question.

## Use design principles as questions

| Question | Consequence for this change |
| --- | --- |
| Does this unit mix independent reasons to change? | Separate independently evolving rules; retain cohesive calculations even if they contain several steps. |
| Is extension needed now? | Implement supported variation with the smallest mechanism. Do not add a registry for a hypothetical next format. |
| Can implementations preserve the same behavioral contract? | Include errors, side effects, ordering, and lifecycle in the contract; matching method signatures is insufficient. |
| Do callers depend only on capabilities they use? | Offer the needed operation without exposing internal collaborators merely for tests. |
| Does dependency direction protect the project's established boundaries? | Keep dependencies consistent with the existing framework and architecture. Introduce translation where it protects an actual responsibility. |

Shared code should represent a decision that must change consistently. Identical arithmetic for two independently governed business rules may belong in two places. Conversely, renaming copies does not make a shared contract independent.

Choose tests at the boundary whose behavior matters. Keep focused tests for independently meaningful algorithms and integration coverage for actual wiring. Moving code behind a larger interface does not prove all smaller tests redundant; compare the regressions they detect before retiring them.

## Example: one-format CSV export

For an application with conventional controllers and jobs, put shipment export in one cohesive shared implementation using the standard CSV writer. Give both callers validated settings, preserve tracking numbers as strings, and let each caller's reporting boundary handle contextual failures. Verify literal expected CSV, including `00123`, independently of the writer.

A serializer registry and forwarding service chain have no current job here. An existing provider adapter that enforces rate limits and translates errors does. Keep the HTTP and job adapters responsible for their entry-specific guarantees, even when they share export logic.

Provenance and deliberate adaptations: [Sources](../SOURCES.md).
