# Sources and Adaptation Decisions

Reviewed **2026-10-19**. Read this file when maintaining the skill or inspecting attribution; it is not required for ordinary implementation work.

## Provenance

The starting point was the user-provided `scratch/Coding-Practices-Skill.md` draft. Its background list named Codebase Design, Clean Code, and Search First. The original drafting date and upstream revisions were not recorded. This review checked those entrypoints and the selected supporting material below; it does not establish which supporting files were consulted during drafting.

The published workflow and references are locally authored synthesis of the draft and reviewed ideas. No complete upstream workflow, script, template, or asset is bundled. All instructions required at runtime are local; upstream links provide attribution and maintenance context. The local MIT metadata does not relicense linked material.

Links below track mutable `main` branches. Commit lookups were unavailable through the web tool; a local GitHub CLI connection check also failed. No immutable revisions are claimed. The Clean Code entrypoint declared version `4.0.0`; the other two entrypoints declared no version. These describe inspected sources, not dependency recommendations. Record a verified commit and inspect its exact files when updating.

## Source Register

| Source inspected | Local use | Adaptation |
| --- | --- | --- |
| Matt Pocock, [Codebase Design](https://github.com/mattpocock/skills/blob/main/skills/engineering/codebase-design/SKILL.md) | [Module design](references/module-design.md): information hidden from callers and useful ownership | Use project terminology. One implementation can justify a real boundary; interface size and adapter count are not automatic verdicts. |
| Battseren Badral / btseee, [Clean Code](https://github.com/btseee/clean-code-skills/blob/main/skills/clean-code/SKILL.md) | Core workflow, placement, boundary handling, and module-design questions | Follow established framework architecture. Omitted mandatory `.clean/` state, detection scripts, language packs, universal layer prescriptions, and a required TDD ritual for every edit. |
| Affaan Mustafa / affaan-m, [Search First](https://github.com/affaan-m/ECC/blob/main/skills/search-first/SKILL.md) | [Reuse and dependencies](references/reuse-and-dependencies.md): evaluate reuse, adoption, adaptation, and local implementation | Research depth follows the decision. Omitted mandatory researcher agents, MCP/skill discovery for unrelated tasks, package catalogs, and numerical candidate scores. Verify APIs against the actual version. |

## Supporting Material Reviewed

| File and inspection scope | Contribution and boundary |
| --- | --- |
| Codebase Design [DEEPENING.md](https://github.com/mattpocock/skills/blob/main/skills/engineering/codebase-design/DEEPENING.md) | Module-design reference: consider dependencies and tests when changing a boundary. Did not adopt automatic deletion of lower-level tests, automatic merging of in-process code, or a two-adapter requirement. |
| Clean Code [references/architecture.md](https://github.com/btseee/clean-code-skills/blob/main/skills/clean-code/references/architecture.md), opening sections through SOLID | Module-design reference: independent reasons to change, substitutability, caller needs, and dependency direction. The full file was not reviewed. Its architectural prescriptions are not imposed on every framework. |

Other upstream reference catalogs were not exhaustively reviewed or bundled. The one-format CSV example and core testing guidance came from the supplied draft. There is no required invocation of `project-cleanup`, Codebase Design, or any other sibling skill. No measured effectiveness claim is made.

## License Status

- Codebase Design repository: [MIT](https://github.com/mattpocock/skills/blob/main/LICENSE), copyright 2026 Matt Pocock.
- Clean Code repository: [MIT](https://github.com/btseee/clean-code-skills/blob/main/LICENSE), copyright 2026 Battseren Badral.
- Search First repository: [MIT](https://github.com/affaan-m/ECC/blob/main/LICENSE), copyright 2026 Affaan Mustafa.

The linked license files were inspected for attribution. Future verbatim reuse should retain the notices required for the material actually copied.

## Maintenance

Update from a concrete task failure or relevant source change. Reopen the affected source and supporting files, record what was inspected, and preserve the adaptation decisions above. If incorporating an upstream workflow later, include its necessary guidance locally with required notices instead of introducing a separately installed skill dependency.

Keep the core workflow small and load references only when their decisions arise. Check metadata, local links, and a dry-run installation. Useful evaluation cases include a local bug fix, a useful single-implementation adapter, identical code for independently governed rules, a dependency choice with an unavailable API check, and a read-only review. Do not claim those evaluations ran unless their outcomes were inspected.
